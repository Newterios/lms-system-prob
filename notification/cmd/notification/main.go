// notification-svc-v2 — NATS subscriber + AP4 worker pool + dead-letter.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Newterios/lms-system-prob/notification/internal/jobqueue"
	"github.com/Newterios/lms-system-prob/notification/internal/mailer"
	"github.com/Newterios/lms-system-prob/notification/internal/subscriber"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	natsURL := mustEnv("NATS_URL")
	gatewayURL := envOr("GATEWAY_URL", "http://localhost:8090")
	poolSize := parseInt(envOr("WORKER_POOL_SIZE", "3"), 3)
	maxRetries := parseInt(envOr("JOB_RETRY_ATTEMPTS", "3"), 3)
	dlqKey := envOr("DLQ_KEY", "dlq:notification")
	mailerKind := envOr("MAILER", "mock")

	slog.Info("notification-svc-v2 starting",
		"nats", natsURL,
		"mailer", mailerKind,
		"pool_size", poolSize,
		"max_retries", maxRetries,
	)

	// ── Mailer ─────────────────────────────────────────────────────────────────
	var m jobqueue.Mailer
	switch mailerKind {
	case "smtp":
		cfg := mailer.Config{
			Host:     envOr("SMTP_HOST", "smtp.gmail.com"),
			Port:     envOr("SMTP_PORT", "587"),
			Username: mustEnv("SMTP_USERNAME"),
			Password: mustEnv("SMTP_PASSWORD"),
			From:     envOr("SMTP_FROM", mustEnv("SMTP_USERNAME")),
		}
		m = mailer.NewSMTP(cfg)
		slog.Info("SMTP mailer configured", "host", cfg.Host, "from", cfg.From)
	default:
		m = mailer.NewMock(gatewayURL)
		slog.Info("mock mailer configured", "gateway", gatewayURL)
	}

	// ── Redis DLQ writer ───────────────────────────────────────────────────────
	var dlq jobqueue.DLQWriter
	if redisURL := envOr("REDIS_URL", ""); redisURL != "" {
		opts, err := goredis.ParseURL(redisURL)
		if err != nil {
			slog.Warn("Redis URL parse failed, DLQ will go to stderr only", "err", err)
		} else {
			rc := goredis.NewClient(opts)
			dlq = &redisDLQ{rc}
			slog.Info("Redis DLQ connected", "key", dlqKey)
		}
	} else {
		slog.Warn("REDIS_URL not set — DLQ entries go to stderr only")
	}

	// ── Worker pool ────────────────────────────────────────────────────────────
	pool := jobqueue.New(jobqueue.Config{
		PoolSize:   poolSize,
		MaxRetries: maxRetries,
		DLQKey:     dlqKey,
	}, m, dlq, slog.Default())

	sub, err := subscriber.New(natsURL, pool, slog.Default())
	if err != nil {
		slog.Error("NATS subscriber failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	slog.Info("notification-svc-v2 ready")
	<-ctx.Done()
	slog.Info("shutting down")
	sub.Drain(context.Background())
	slog.Info("done")
}

// redisDLQ wraps go-redis to implement jobqueue.DLQWriter.
type redisDLQ struct{ client *goredis.Client }

func (r *redisDLQ) LPush(ctx context.Context, key, value string) error {
	return r.client.LPush(ctx, key, value).Err()
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "fatal: required env var %s is not set\n", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
