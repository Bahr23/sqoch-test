package usecase

import (
	"context"
	"github.com/google/uuid"
	"sqoch-ticket/backend/internal/domain"
)

type EventRepository interface {
	Events(context.Context) ([]domain.Event, error)
	Event(context.Context, string) (domain.Event, error)
	SaveEvent(context.Context, string, domain.EventInput, bool) (domain.Event, error)
	DeleteEvent(context.Context, string) error
}
type Events struct{ repository EventRepository }

func NewEvents(r EventRepository) *Events                          { return &Events{repository: r} }
func (e *Events) List(ctx context.Context) ([]domain.Event, error) { return e.repository.Events(ctx) }
func (e *Events) Get(ctx context.Context, id string) (domain.Event, error) {
	if !validID(id) {
		return domain.Event{}, domain.ErrNotFound
	}
	return e.repository.Event(ctx, id)
}
func (e *Events) Save(ctx context.Context, id string, input domain.EventInput) (domain.Event, error) {
	create := id == ""
	if !create && !validID(id) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err := input.Validate(); err != nil {
		return domain.Event{}, err
	}
	if create {
		id = uuid.NewString()
	}
	return e.repository.SaveEvent(ctx, id, input, create)
}
func (e *Events) Delete(ctx context.Context, id string) error {
	if !validID(id) {
		return domain.ErrNotFound
	}
	return e.repository.DeleteEvent(ctx, id)
}
func validID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}
