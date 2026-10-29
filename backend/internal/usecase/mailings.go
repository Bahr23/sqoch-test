package usecase

import (
	"context"
	"github.com/google/uuid"
	"sqoch-ticket/backend/internal/domain"
)

type MailingRepository interface {
	Mailings(context.Context) ([]domain.Mailing, error)
	Mailing(context.Context, string) (domain.MailingDetails, error)
	SaveMailing(context.Context, string, domain.MailingInput, bool) (domain.MailingDetails, error)
	MailingRecipients(context.Context, string) ([]domain.MailingRecipient, error)
	ConfirmMailing(context.Context, string, domain.MailingConfirmation) (domain.MailingDetails, error)
	ResumeMailing(context.Context, string) (domain.MailingDetails, error)
}

func (m *Mailings) Confirm(ctx context.Context, id string, c domain.MailingConfirmation) (domain.MailingDetails, error) {
	if !validID(id) {
		return domain.MailingDetails{}, domain.ErrNotFound
	}
	return m.repository.ConfirmMailing(ctx, id, c)
}
func (m *Mailings) Resume(ctx context.Context, id string) (domain.MailingDetails, error) {
	if !validID(id) {
		return domain.MailingDetails{}, domain.ErrNotFound
	}
	return m.repository.ResumeMailing(ctx, id)
}

type Mailings struct{ repository MailingRepository }

func NewMailings(r MailingRepository) *Mailings { return &Mailings{repository: r} }
func (m *Mailings) List(ctx context.Context) ([]domain.Mailing, error) {
	return m.repository.Mailings(ctx)
}
func (m *Mailings) Get(ctx context.Context, id string) (domain.MailingDetails, error) {
	if !validID(id) {
		return domain.MailingDetails{}, domain.ErrNotFound
	}
	return m.repository.Mailing(ctx, id)
}
func (m *Mailings) Recipients(ctx context.Context, eventID string) ([]domain.MailingRecipient, error) {
	if !validID(eventID) {
		return nil, domain.ErrNotFound
	}
	return m.repository.MailingRecipients(ctx, eventID)
}
func (m *Mailings) Save(ctx context.Context, id string, input domain.MailingInput) (domain.MailingDetails, error) {
	create := id == ""
	if !create && !validID(id) {
		return domain.MailingDetails{}, domain.ErrNotFound
	}
	if err := input.Validate(); err != nil {
		return domain.MailingDetails{}, err
	}
	if !validID(input.EventID) {
		return domain.MailingDetails{}, &domain.ValidationError{Fields: map[string]string{"event_id": "Выберите ивент для рассылки."}}
	}
	if create {
		id = uuid.NewString()
	}
	return m.repository.SaveMailing(ctx, id, input, create)
}
