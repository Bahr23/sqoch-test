package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"testing"
)

func TestEventsIsolateStockAndKeepPriceSnapshots(t *testing.T) {
	_, booking, repo := setup(t)
	ctx := context.Background()
	events := usecase.NewEvents(repo)
	list, err := events.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("seed: %v %v", list, err)
	}
	age := 18
	quote, err := booking.Checkout(ctx, domain.OrderInput{EventID: sqlite.SecondEventID, Email: "a@b.ru", Participants: []domain.ParticipantInput{{Name: "Анна", Age: &age}}})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Total != 2500 {
		t.Fatal("event price ignored")
	}
	e, err := events.Get(ctx, sqlite.SecondEventID)
	if err != nil {
		t.Fatal(err)
	}
	v := e.Input()
	v.FullPrice = 3500
	v.Title = "Новое название"
	if _, err = events.Save(ctx, e.ID, v); err != nil {
		t.Fatal(err)
	}
	order, err := booking.Confirm(ctx, quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Total != 2500 || order.Event.Title == v.Title || order.EventID != sqlite.SecondEventID {
		t.Fatal("snapshot was overwritten")
	}
	first, _ := events.Get(ctx, sqlite.FirstEventID)
	second, _ := events.Get(ctx, sqlite.SecondEventID)
	if first.Remaining != 100 || second.Remaining != 59 || second.Sold != 1 {
		t.Fatalf("stock leaked: %+v %+v", first, second)
	}
	v.Capacity = 2
	reduced, err := events.Save(ctx, e.ID, v)
	if err != nil || reduced.Remaining != 1 {
		t.Fatalf("capacity adjustment: %+v %v", reduced, err)
	}
	// Sell the remaining seat, then refuse a capacity below the number sold.
	next, err := booking.Checkout(ctx, domain.OrderInput{EventID: e.ID, Email: "a@b.ru", Participants: []domain.ParticipantInput{{Name: "Анна", Age: &age}}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Total != 3500 {
		t.Fatal("new price not applied")
	}
	if _, err = booking.Confirm(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	v.Capacity = 1
	if _, err = events.Save(ctx, e.ID, v); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("capacity below sales accepted: %v", err)
	}
	if err = events.Delete(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = events.Get(ctx, e.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted event public")
	}
	again, err := booking.Confirm(ctx, quote.ID)
	if err != nil || again.ID != order.ID {
		t.Fatal("deletion damaged existing order")
	}
	if _, err = booking.Checkout(ctx, domain.OrderInput{EventID: e.ID, Email: "a@b.ru", Participants: []domain.ParticipantInput{{Name: "Анна", Age: &age}}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted event accepts checkout")
	}
}
func TestDeletedEventCannotConfirmPendingCheckout(t *testing.T) {
	_, booking, repo := setup(t)
	ctx := context.Background()
	q := draft(t, booking, false)
	if err := repo.DeleteEvent(ctx, sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := booking.Confirm(ctx, q.ID); !errors.Is(err, domain.ErrSoldOut) {
		t.Fatalf("deleted event confirmed: %v", err)
	}
}
func TestMigrationPreservesLegacyOrderAndDoesNotReseedDeletedEvents(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
 CREATE TABLE events(id TEXT PRIMARY KEY,title TEXT NOT NULL,starts_at TEXT NOT NULL,venue TEXT NOT NULL,remaining INTEGER NOT NULL CHECK(remaining>=0));
 CREATE TABLE checkouts(id TEXT PRIMARY KEY,event_id TEXT NOT NULL REFERENCES events(id),email TEXT NOT NULL,participants TEXT NOT NULL,total INTEGER NOT NULL CHECK(total>0),expires_at TEXT NOT NULL,simulate_sold_out INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE orders(id TEXT PRIMARY KEY,checkout_id TEXT NOT NULL UNIQUE REFERENCES checkouts(id),number TEXT NOT NULL,created_at TEXT NOT NULL);
 INSERT INTO events VALUES('orbit','Старое событие','2026-11-21T18:00:00+03:00','Москва',98);
 INSERT INTO checkouts VALUES('legacy-checkout','orbit','guest@example.ru','[{"name":"Анна","age":18,"ticket_type":"full","price":2000},{"name":"Маша","age":13,"ticket_type":"child","price":1000}]',3000,'2026-10-01T10:00:00Z',0);
 INSERT INTO orders VALUES('legacy-order','legacy-checkout','SQ-LEGACY','2026-10-01T09:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewBookingRepository(db)
	event, err := repo.Event(ctx, sqlite.FirstEventID)
	if err != nil || event.Title != "Старое событие" || event.Remaining != 98 || event.Capacity != 100 {
		t.Fatalf("migration damaged event: %+v %v", event, err)
	}
	b := usecase.NewBooking(repo, true)
	order, err := b.Confirm(ctx, "legacy-checkout")
	if err != nil || order.ID != "legacy-order" || order.Event.ID != sqlite.FirstEventID || order.Total != 3000 || order.Status != domain.OrderActive {
		t.Fatalf("migration damaged order: %+v %v", order, err)
	}
	if err = repo.DeleteEvent(ctx, sqlite.SecondEventID); err != nil {
		t.Fatal(err)
	}
	if err = sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	list, err := repo.Events(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal("seed repeated")
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("broken foreign keys")
	}
}
