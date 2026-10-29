package usecase

import (
	"context"
	"crypto/rand"
	"sqoch-ticket/backend/internal/domain"
	"strings"
	"time"
)

type BookingRepository interface {
	Events(context.Context) ([]domain.Event, error)
	Event(context.Context, string) (domain.Event, error)
	CreateCheckout(context.Context, domain.Checkout, bool) error
	Confirm(context.Context, string, time.Time) (domain.Order, error)
}
type Booking struct {
	repository BookingRepository
	demo       bool
}

func NewBooking(repository BookingRepository, demo bool) *Booking {
	return &Booking{repository: repository, demo: demo}
}
func (b *Booking) Event(ctx context.Context, id string) (domain.Event, error) {
	if !validID(id) {
		return domain.Event{}, domain.ErrNotFound
	}
	event, err := b.repository.Event(ctx, id)
	event.DemoEnabled = b.demo
	return event, err
}
func (b *Booking) Checkout(ctx context.Context, input domain.OrderInput) (domain.Checkout, error) {
	if input.SimulateSoldOut && !b.demo {
		return domain.Checkout{}, &domain.ValidationError{Fields: map[string]string{"scenario": "Демонстрационный сценарий отключён."}}
	}
	if !validID(input.EventID) {
		return domain.Checkout{}, domain.ErrNotFound
	}
	event, err := b.repository.Event(ctx, input.EventID)
	if err != nil {
		return domain.Checkout{}, err
	}
	participants, total, err := domain.PriceOrder(input, event)
	if err != nil {
		return domain.Checkout{}, err
	}
	if event.Remaining < len(participants) {
		return domain.Checkout{}, domain.ErrSoldOut
	}
	checkout := domain.Checkout{EventID: event.ID, Event: &event, ID: rand.Text(), Email: strings.TrimSpace(input.Email), Participants: participants, Total: total, ExpiresAt: time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339)}
	if err := b.repository.CreateCheckout(ctx, checkout, input.SimulateSoldOut); err != nil {
		return domain.Checkout{}, err
	}
	return checkout, nil
}
func (b *Booking) Confirm(ctx context.Context, id string) (domain.Order, error) {
	return b.repository.Confirm(ctx, id, time.Now().UTC())
}

func (b *Booking) Events(ctx context.Context) ([]domain.Event, error) {
	events, err := b.repository.Events(ctx)
	for i := range events {
		events[i].DemoEnabled = b.demo
	}
	return events, err
}
