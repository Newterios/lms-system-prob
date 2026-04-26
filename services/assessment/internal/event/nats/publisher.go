package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

// Publisher implements port.EventPublisher via NATS Core.
// For assessment-svc, critical events (submitted, graded) go through the
// Outbox+relay pattern on top — this publisher is used by the relay goroutine
// and for best-effort events like assessment.attempt.started.
type Publisher struct {
	nc *nats.Conn
}

func New(url string) (*Publisher, error) {
	nc, err := nats.Connect(url,
		nats.Name("assessment-svc-v2"),
		nats.MaxReconnects(10),
		nats.ReconnectWait(nats.DefaultReconnectWait),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return &Publisher{nc: nc}, nil
}

func (p *Publisher) Publish(_ context.Context, subject string, payload []byte) error {
	return p.nc.Publish(subject, payload)
}

func (p *Publisher) Close() {
	_ = p.nc.Drain()
}
