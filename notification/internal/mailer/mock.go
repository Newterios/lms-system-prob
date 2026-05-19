package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Mock sends notifications to the AP4 mock HTTP gateway (§4.4).
// This is the default for MAILER=mock / local dev — no real email is sent.
type Mock struct {
	gatewayURL string
	client     *http.Client
}

// NewMock constructs a Mock mailer pointing at gatewayURL (e.g. http://localhost:8090).
func NewMock(gatewayURL string) *Mock {
	return &Mock{
		gatewayURL: gatewayURL,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

type mockGatewayRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	EventType      string `json:"event_type"`
	Payload        string `json:"payload"` // hex-encoded raw bytes
}

// Deliver POSTs the event to the mock-gateway /notify endpoint.
func (m *Mock) Deliver(_ context.Context, eventType string, payload []byte) error {
	body := mockGatewayRequest{
		IdempotencyKey: idempotencyKey(eventType, payload),
		EventType:      eventType,
		Payload:        fmt.Sprintf("%x", payload),
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := m.client.Post(m.gatewayURL+"/notify", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock-gateway returned %d", resp.StatusCode)
	}
	return nil
}

func idempotencyKey(eventType string, payload []byte) string {
	// Stable key: event type + hex payload (matches legacy pool logic).
	return fmt.Sprintf("%x", []byte(fmt.Sprintf("%s|%x", eventType, payload)))
}
