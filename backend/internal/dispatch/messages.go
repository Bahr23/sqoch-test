// Package dispatch defines versioned wire contracts between independently
// deployed backend and notifier processes. It contains no storage or transport.
package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/mail"
	"strings"
)

var ErrPoison = errors.New("invalid dispatch message")

type Command struct {
	Schema     int    `json:"schema"`
	ID         string `json:"id"`
	MailingID  string `json:"mailing_id"`
	EventID    string `json:"event_id"`
	Email      string `json:"email"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	Generation int    `json:"generation"`
}

func (c Command) Valid() bool {
	_, a := uuid.Parse(c.ID)
	_, b := uuid.Parse(c.MailingID)
	_, d := uuid.Parse(c.EventID)
	address, e := mail.ParseAddress(c.Email)
	return c.Schema == 1 && a == nil && b == nil && d == nil && e == nil && address.Address == c.Email && c.Email == strings.ToLower(strings.TrimSpace(c.Email)) && c.Generation >= 0 && c.Subject != "" && c.Body != ""
}
func (c Command) Fingerprint() string {
	c.Generation = 0
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type Result struct {
	Schema     int    `json:"schema"`
	ID         string `json:"id"`
	Generation int    `json:"generation"`
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	Error      string `json:"error"`
	UpdatedAt  string `json:"updated_at"`
}

func (r Result) Valid() bool {
	_, err := uuid.Parse(r.ID)
	return r.Schema == 1 && err == nil && r.Generation >= 0 && r.Version > 0 && r.Attempts >= 0 && ValidStatus(r.Status)
}
func ValidStatus(s string) bool {
	switch s {
	case "pending", "processing", "retry_wait", "sent", "skipped", "failed":
		return true
	}
	return false
}
