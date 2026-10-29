package sqlite_test

import (
	"context"
	"errors"
	"slices"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"testing"
	"time"
)

func TestMailingRecipientsOnlyUseActiveOrdersAndDeduplicateEmail(t *testing.T) {
	db, booking, _ := setup(t)
	ctx := context.Background()
	if _, err := sqlite.SeedDemoOrders(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	buy := func(eventID, email, name string) domain.Order {
		t.Helper()
		age := 30
		q, err := booking.Checkout(ctx, domain.OrderInput{EventID: eventID, Email: email, Participants: []domain.ParticipantInput{{Name: name, Age: &age}}})
		if err != nil {
			t.Fatal(err)
		}
		o, err := booking.Confirm(ctx, q.ID)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	duplicate := buy(sqlite.FirstEventID, " ANNA.DEMO@EXAMPLE.COM ", "Повторный покупатель")
	cancelled := buy(sqlite.FirstEventID, "cancelled-only@example.com", "Отменённый участник")
	if _, err := db.Exec("UPDATE orders SET status='cancelled',cancelled_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339), cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE events SET remaining=remaining+1 WHERE id=?", sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	buy(sqlite.SecondEventID, "second-only@example.com", "Другой ивент")
	age := 30
	if _, err := booking.Checkout(ctx, domain.OrderInput{EventID: sqlite.FirstEventID, Email: "pending@example.com", Participants: []domain.ParticipantInput{{Name: "Неподтверждённый", Age: &age}}}); err != nil {
		t.Fatal(err)
	}
	mailings := usecase.NewMailings(sqlite.NewMailingRepository(db))
	recipients, err := mailings.Recipients(ctx, sqlite.FirstEventID)
	if err != nil || len(recipients) != 6 {
		t.Fatalf("recipients: %+v %v", recipients, err)
	}
	for _, r := range recipients {
		if r.Email == "pending@example.com" || r.Email == "cancelled-only@example.com" || r.Email == "second-only@example.com" {
			t.Fatal("ineligible recipient", r)
		}
		if slices.Contains(r.Names, "София Смирнова") || slices.Contains(r.Names, "Виктор Соколов") {
			t.Fatal("cancelled participants included", r)
		}
		if r.Email == "anna.demo@example.com" && (r.OrderCount != 3 || len(r.Names) != 3 || !slices.Contains(r.Names, "Повторный покупатель")) {
			t.Fatal("email not deduplicated", r)
		}
	}
	draft, err := mailings.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID, Body: "  Встречаемся завтра!\nНачало в 18:00.  "})
	if err != nil || draft.Status != "draft" || len(draft.Recipients) != 6 || draft.Body != "Встречаемся завтра!\nНачало в 18:00." {
		t.Fatalf("draft: %+v %v", draft, err)
	}
	// Saved drafts use the current audience, not a stale copy of contact data.
	if _, err := db.Exec("UPDATE orders SET status='cancelled',cancelled_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339), duplicate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE events SET remaining=remaining+1 WHERE id=?", sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	loaded, err := mailings.Get(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range loaded.Recipients {
		if slices.Contains(r.Names, "Повторный покупатель") {
			t.Fatal("stale recipient snapshot")
		}
	}
	updated, err := mailings.Save(ctx, draft.ID, domain.MailingInput{EventID: sqlite.SecondEventID, Body: "Другой текст"})
	if err != nil || updated.ID != draft.ID || updated.CreatedAt != draft.CreatedAt || updated.Body != "Другой текст" || updated.EventID != sqlite.SecondEventID || len(updated.Recipients) != 7 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	list, err := mailings.List(ctx)
	if err != nil || len(list) != 1 || list[0].Body != "Другой текст" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestDraftCanBeIncompleteAndSurvivesEventDeletion(t *testing.T) {
	db, _, repo := setup(t)
	ctx := context.Background()
	m := usecase.NewMailings(sqlite.NewMailingRepository(db))
	people, err := m.Recipients(ctx, sqlite.FirstEventID)
	if err != nil || people == nil || len(people) != 0 {
		t.Fatal("empty audience must be an array")
	}
	draft, err := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID})
	if err != nil || draft.Body != "" || draft.Status != "draft" {
		t.Fatalf("incomplete draft: %v", err)
	}
	if err := repo.DeleteEvent(ctx, sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	loaded, err := m.Get(ctx, draft.ID)
	if err != nil || loaded.EventAvailable || len(loaded.Recipients) != 0 {
		t.Fatalf("deleted event draft lost: %+v %v", loaded, err)
	}
	if _, err := m.Recipients(ctx, sqlite.FirstEventID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted event audience available")
	}
	_, err = m.Save(ctx, draft.ID, domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Keep"})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatal("deleted event accepted")
	}
	if _, err := m.Save(ctx, draft.ID, domain.MailingInput{EventID: sqlite.SecondEventID, Body: "Новый ивент"}); err != nil {
		t.Fatal(err)
	}
}
