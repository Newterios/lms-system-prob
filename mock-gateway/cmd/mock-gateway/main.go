// mock-gateway — AP4 §4.4 fake external notification API.
//
// POST /notify
//   - Accepts: {"idempotency_key":"<sha256>","event_type":"<subject>","payload":"<base64>"}
//   - 20 % random 503 (configurable via MOCK_GATEWAY_FAILURE_RATE env, 0–100)
//   - Idempotent: same idempotency_key returns 200 without re-processing
//
// GET  /log                     — returns the last 200 notification log entries as JSON array
// GET  /healthz                 — returns 200 OK {"status":"ok"}
// POST /admin/set-failure-rate  — {"rate":0} sets failure rate at runtime (for demo)
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// NotifyRequest is the body sent by the notification worker.
type NotifyRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	EventType      string `json:"event_type"`
	Payload        string `json:"payload"` // base64-encoded original NATS payload
}

// LogEntry records one notification attempt.
type LogEntry struct {
	At             time.Time `json:"at"`
	IdempotencyKey string    `json:"idempotency_key"`
	EventType      string    `json:"event_type"`
	Status         int       `json:"status"` // 200 or 503
	Duplicate      bool      `json:"duplicate"`
}

var (
	mu          sync.Mutex
	seen        = map[string]bool{}  // idempotency store (in-memory, resets on restart)
	logEntries  = make([]LogEntry, 0, 200)
	failureRate int // percent, 0–100
)

const maxLog = 200

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := envOr("MOCK_GATEWAY_PORT", "8090")
	failureRate = parseInt(envOr("MOCK_GATEWAY_FAILURE_RATE", "20"), 20)
	slog.Info("mock-gateway starting", "port", port, "failure_rate_pct", failureRate)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /notify", handleNotify)
	mux.HandleFunc("GET /log", handleLog)
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("POST /admin/set-failure-rate", handleSetFailureRate)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
}

func handleNotify(w http.ResponseWriter, r *http.Request) {
	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// idempotency check
	if seen[req.IdempotencyKey] {
		slog.Info("duplicate notification skipped", "key", req.IdempotencyKey, "event", req.EventType)
		appendLog(req, 200, true)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "duplicate"})
		return
	}

	// random failure simulation
	if failureRate > 0 && rand.Intn(100) < failureRate {
		slog.Warn("mock-gateway simulating failure", "event", req.EventType)
		appendLog(req, 503, false)
		http.Error(w, "simulated gateway error", http.StatusServiceUnavailable)
		return
	}

	// success
	seen[req.IdempotencyKey] = true
	slog.Info("notification received", "event", req.EventType, "key", req.IdempotencyKey)
	appendLog(req, 200, false)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleLog(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logEntries)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
}

func handleSetFailureRate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rate int `json:"rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Rate < 0 || body.Rate > 100 {
		http.Error(w, "rate must be 0-100", http.StatusBadRequest)
		return
	}
	mu.Lock()
	old := failureRate
	failureRate = body.Rate
	mu.Unlock()
	slog.Info("failure rate updated", "old", old, "new", body.Rate)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "rate": body.Rate})
}

// appendLog is called with mu held.
func appendLog(req NotifyRequest, status int, dup bool) {
	if len(logEntries) >= maxLog {
		logEntries = logEntries[1:] // ring buffer
	}
	logEntries = append(logEntries, LogEntry{
		At:             time.Now().UTC(),
		IdempotencyKey: req.IdempotencyKey,
		EventType:      req.EventType,
		Status:         status,
		Duplicate:      dup,
	})
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
