package noop

import "context"

// Publisher is a no-op event publisher. In Phase 2 this is replaced by NATS.
type Publisher struct{}

func New() *Publisher { return &Publisher{} }

func (p *Publisher) Publish(_ context.Context, _ string, _ []byte) error { return nil }
