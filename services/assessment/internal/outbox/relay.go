package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/usecase/port"
)

// Relay polls the outbox table and publishes events via EventPublisher.
// In Phase 1 the publisher is a noop so it just marks rows as published.
// In Phase 2 the noop is replaced with a real NATS publisher — relay code
// does not change (Open/Closed principle).
type Relay struct {
	repo  port.OutboxRepository
	pub   port.EventPublisher
	tick  time.Duration // default 200ms
	batch int           // default 100
	log   *slog.Logger
}

// Config holds Relay tuning knobs.
type Config struct {
	TickInterval time.Duration // 0 → 200ms
	BatchSize    int           // 0 → 100
}

// NewRelay constructs a Relay.
func NewRelay(repo port.OutboxRepository, pub port.EventPublisher, cfg Config, log *slog.Logger) *Relay {
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = 200 * time.Millisecond
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = 100
	}
	return &Relay{repo: repo, pub: pub, tick: tick, batch: batch, log: log}
}

// Run starts the relay loop. It returns when ctx is cancelled.
// Call this in a goroutine; the returned error is always ctx.Err().
func (r *Relay) Run(ctx context.Context) error {
	t := time.NewTicker(r.tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.flush(ctx)
		}
	}
}

func (r *Relay) flush(ctx context.Context) {
	entries, err := r.repo.ListUnpublished(ctx, r.batch)
	if err != nil {
		r.log.Warn("relay: list unpublished failed", "err", err)
		return
	}
	for _, e := range entries {
		if err := r.pub.Publish(ctx, e.EventType, e.Payload); err != nil {
			r.log.Warn("relay: publish failed", "id", e.ID, "event", e.EventType, "err", err)
			continue // leave unpublished; next tick retries
		}
		if err := r.repo.MarkPublished(ctx, e.ID, time.Now().UTC()); err != nil {
			r.log.Warn("relay: mark published failed", "id", e.ID, "err", err)
		}
	}
}
