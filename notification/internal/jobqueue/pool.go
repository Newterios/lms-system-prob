package jobqueue

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// Job represents a notification task decoded from a NATS event.
type Job struct {
	EventType  string
	EntityID   string // used for idempotency key
	OccurredAt time.Time
	Payload    []byte
}

// IdempotencyKey returns SHA256(EventType|EntityID|OccurredAt).
func (j Job) IdempotencyKey() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", j.EventType, j.EntityID, j.OccurredAt.UTC().Format(time.RFC3339Nano))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Mailer delivers a single notification job to whatever backend is configured.
// Implementations live in notification/internal/mailer (smtp or mock).
type Mailer interface {
	Deliver(ctx context.Context, eventType string, payload []byte) error
}

// DLQWriter stores dead-letter entries. Implemented by the Redis client in main.
type DLQWriter interface {
	LPush(ctx context.Context, key string, value string) error
}

// Config holds worker pool configuration.
type Config struct {
	PoolSize   int
	MaxRetries int
	DLQKey     string
}

// Pool is a fixed-size worker pool that dispatches notification jobs via a Mailer.
type Pool struct {
	cfg    Config
	jobs   chan Job
	dlq    DLQWriter
	mailer Mailer
	log    *slog.Logger
}

// New creates a Pool and starts cfg.PoolSize workers immediately.
func New(cfg Config, mailer Mailer, dlq DLQWriter, log *slog.Logger) *Pool {
	p := &Pool{
		cfg:    cfg,
		jobs:   make(chan Job, cfg.PoolSize*10),
		dlq:    dlq,
		mailer: mailer,
		log:    log,
	}
	for i := 0; i < cfg.PoolSize; i++ {
		go p.worker(i)
	}
	return p
}

// Submit enqueues a job non-blocking; drops if the channel is full.
func (p *Pool) Submit(j Job) {
	select {
	case p.jobs <- j:
	default:
		p.log.Warn("job queue full, dropping job", "event", j.EventType)
	}
}

func (p *Pool) worker(id int) {
	p.log.Info("notification worker started", "id", id)
	for j := range p.jobs {
		p.dispatch(j)
	}
}

func (p *Pool) dispatch(j Job) {
	key := j.IdempotencyKey()

	var lastErr error
	for attempt := 0; attempt <= p.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1)) * float64(200*time.Millisecond))
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			time.Sleep(backoff)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.mailer.Deliver(ctx, j.EventType, j.Payload)
		cancel()

		if err == nil {
			p.log.Info("notification delivered", "event", j.EventType, "key", key, "attempt", attempt)
			return
		}
		lastErr = err
		p.log.Warn("notification failed", "event", j.EventType, "attempt", attempt, "err", err)
	}

	// Dead-letter: push to Redis and log to stderr.
	p.log.Error("notification dead-lettered", "event", j.EventType, "key", key, "err", lastErr)
	dlqEntry, _ := json.Marshal(map[string]any{
		"event_type":      j.EventType,
		"idempotency_key": key,
		"payload":         fmt.Sprintf("%x", j.Payload),
		"failed_at":       time.Now().UTC(),
		"error":           lastErr.Error(),
	})
	if p.dlq != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := p.dlq.LPush(ctx, p.cfg.DLQKey, string(dlqEntry)); err != nil {
			p.log.Error("dlq push failed", "err", err)
		}
	}
	fmt.Printf(`{"level":"ERROR","time":"%s","msg":"dead_letter","event_type":"%s","key":"%s"}%s`,
		time.Now().UTC().Format(time.RFC3339), j.EventType, key, "\n")
}
