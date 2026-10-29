package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrMailingLocked = errors.New("mailing already confirmed")
var ErrMailingChanged = errors.New("mailing confirmation is stale")

type MailingInput struct {
	EventID string `json:"event_id"`
	Body    string `json:"body"`
}

func (v *MailingInput) Validate() error {
	v.EventID = strings.TrimSpace(v.EventID)
	v.Body = strings.TrimSpace(v.Body)
	if utf8.RuneCountInString(v.Body) > 10000 {
		return &ValidationError{Fields: map[string]string{"body": "Текст должен быть не длиннее 10 000 символов."}}
	}
	return nil
}

type Mailing struct {
	ID             string `json:"id"`
	EventID        string `json:"event_id"`
	EventTitle     string `json:"event_title"`
	EventAvailable bool   `json:"event_available"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ConfirmedAt    string `json:"confirmed_at"`
}

// Names belong to participants; the contact email belongs to the whole order.
type MailingRecipient struct {
	Email      string   `json:"email"`
	Names      []string `json:"names"`
	OrderCount int      `json:"order_count"`
}

type MailingDetails struct {
	Mailing
	Recipients   []MailingRecipient `json:"recipients"`
	AudienceHash string             `json:"audience_hash"`
	Progress     *MailingProgress   `json:"progress,omitempty"`
	Deliveries   []MailingDelivery  `json:"deliveries,omitempty"`
}

type MailingConfirmation struct {
	ExpectedUpdatedAt string `json:"expected_updated_at"`
	AudienceHash      string `json:"audience_hash"`
}
type MailingProgress struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	Processing int `json:"processing"`
	RetryWait  int `json:"retry_wait"`
	Sent       int `json:"sent"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}
type MailingDelivery struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Names     []string `json:"names"`
	Status    string   `json:"status"`
	Attempts  int      `json:"attempts"`
	Error     string   `json:"error"`
	UpdatedAt string   `json:"updated_at"`
}
