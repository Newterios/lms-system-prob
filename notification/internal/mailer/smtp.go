package mailer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Config holds SMTP connection parameters read from environment variables.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string // display + address, e.g. "EDULMS <foo@gmail.com>"
}

// SMTP delivers notification emails via Gmail STARTTLS.
type SMTP struct {
	cfg Config
}

// NewSMTP constructs an SMTP mailer.
func NewSMTP(cfg Config) *SMTP { return &SMTP{cfg: cfg} }

// smtpEnvelope mirrors the canonical NATS event envelope from all three services.
type smtpEnvelope struct {
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

type smtpEventData struct {
	Email    string  `json:"email"`
	FullName string  `json:"full_name"`
	Title    string  `json:"title"`
	Score    float64 `json:"score"`
}

// Deliver parses the NATS payload, extracts the recipient email from Data, and sends.
// If Data has no "email" field the event is silently skipped (no address to send to).
func (m *SMTP) Deliver(_ context.Context, eventType string, payload []byte) error {
	var env smtpEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("smtp: parse envelope: %w", err)
	}

	var d smtpEventData
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &d)
	}

	if d.Email == "" {
		// Event carries no recipient — nothing to send.
		return nil
	}

	subject, body := formatEmail(eventType, d)
	return m.send(d.Email, subject, body)
}

func (m *SMTP) send(to, subject, body string) error {
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)

	// smtp.SendMail issues STARTTLS automatically when the server advertises it.
	headers := strings.Join([]string{
		"From: " + m.cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
	}, "\r\n")
	return smtp.SendMail(addr, auth, m.cfg.Username, []string{to}, []byte(headers+"\r\n"+body))
}

func formatEmail(eventType string, d smtpEventData) (subject, body string) {
	name := d.FullName
	if name == "" {
		name = d.Email
	}
	switch eventType {
	case "auth.user.registered":
		subject = "Welcome to EDULMS!"
		body = fmt.Sprintf(
			"Hi %s,\n\nWelcome to EDULMS! Your account has been created successfully.\n\nStart exploring courses at http://localhost:4000.\n\nCheers,\nThe EDULMS Team",
			name,
		)
	case "auth.user.verified":
		subject = "Email verified — EDULMS"
		body = fmt.Sprintf(
			"Hi %s,\n\nYour email address has been verified. You now have full access to all features.\n\nCheers,\nThe EDULMS Team",
			name,
		)
	case "auth.password.changed":
		subject = "Your EDULMS password was changed"
		body = fmt.Sprintf(
			"Hi %s,\n\nYour password was changed. If this wasn't you, contact support immediately.\n\nCheers,\nThe EDULMS Team",
			name,
		)
	case "course.enrollment.created":
		title := d.Title
		if title == "" {
			title = "a course"
		}
		subject = fmt.Sprintf("Enrolled in %s — EDULMS", title)
		body = fmt.Sprintf(
			"Hi %s,\n\nYou have been successfully enrolled in \"%s\".\n\nCheers,\nThe EDULMS Team",
			name, title,
		)
	case "assessment.attempt.graded":
		subject = "Your assessment has been graded — EDULMS"
		body = fmt.Sprintf(
			"Hi %s,\n\nYour attempt has been graded. Score: %.1f.\n\nCheers,\nThe EDULMS Team",
			name, d.Score,
		)
	default:
		subject = fmt.Sprintf("EDULMS notification: %s", eventType)
		body = fmt.Sprintf("Hi %s,\n\nYou have a new notification (%s).\n\nCheers,\nThe EDULMS Team", name, eventType)
	}
	return
}
