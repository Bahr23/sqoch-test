package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"sync"
	"testing"
	"time"
)

func setup(t *testing.T) (*sql.DB, *usecase.Booking, *sqlite.BookingRepository) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "booking.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewBookingRepository(db)
	return db, usecase.NewBooking(repo, true), repo
}
func draft(t *testing.T, b *usecase.Booking, simulate bool) domain.Checkout {
	t.Helper()
	age := 18
	q, err := b.Checkout(context.Background(), domain.OrderInput{Email: "guest@example.ru", Participants: []domain.ParticipantInput{{Name: "Гость", Age: &age}}, SimulateSoldOut: simulate})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func TestConfirmationIsIdempotentAndDoesNotReserveAtReview(t *testing.T) {
	db, b, _ := setup(t)
	ctx := context.Background()
	q := draft(t, b, false)
	event, _ := b.Event(ctx)
	if event.Remaining != 100 {
		t.Fatal("review reserved seats")
	}
	order, err := b.Confirm(ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := b.Confirm(ctx, q.ID)
	if err != nil || repeated.ID != order.ID {
		t.Fatalf("duplicate confirmation: %v", err)
	}
	event, _ = b.Event(ctx)
	if event.Remaining != 99 {
		t.Fatalf("remaining %d", event.Remaining)
	}
	if order.Total != 2000 || order.Email != "guest@example.ru" || order.Participants[0].Age != 18 {
		t.Fatalf("invalid order: %+v", order)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	event, _ = b.Event(ctx)
	if event.Remaining != 99 {
		t.Fatal("migration reset inventory")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count); err != nil || count != 1 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
}
func TestConcurrentOrdersCannotOversell(t *testing.T) {
	db, b, _ := setup(t)
	ctx := context.Background()
	if _, err := db.Exec("UPDATE events SET remaining=1"); err != nil {
		t.Fatal(err)
	}
	first, second := draft(t, b, false), draft(t, b, false)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, q := range []domain.Checkout{first, second} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, err := b.Confirm(ctx, id); results <- err }(q.ID)
	}
	wg.Wait()
	close(results)
	success, sold := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrSoldOut) {
			sold++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || sold != 1 {
		t.Fatalf("success=%d sold=%d", success, sold)
	}
	event, _ := b.Event(ctx)
	if event.Remaining != 0 {
		t.Fatal("bad capacity")
	}
}
func TestDemoSoldOutAndExpiredDraftDoNotCreateOrders(t *testing.T) {
	db, b, repo := setup(t)
	ctx := context.Background()
	q := draft(t, b, true)
	if _, err := b.Confirm(ctx, q.ID); !errors.Is(err, domain.ErrSoldOut) {
		t.Fatal(err)
	}
	event, _ := b.Event(ctx)
	if event.Remaining != 100 {
		t.Fatal("demo affected live inventory")
	}
	normal := draft(t, b, false)
	if _, err := repo.Confirm(ctx, normal.ID, time.Now().Add(16*time.Minute)); !errors.Is(err, domain.ErrExpired) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count); err != nil || count != 0 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
	disabled := usecase.NewBooking(repo, false)
	age := 18
	_, err := disabled.Checkout(ctx, domain.OrderInput{Email: "a@b.ru", Participants: []domain.ParticipantInput{{Name: "Анна", Age: &age}}, SimulateSoldOut: true})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatal("demo accepted when disabled")
	}
}
func TestConcurrentRetriesUseOneOrder(t *testing.T) {
	db, b, _ := setup(t)
	q := draft(t, b, false)
	results := make(chan domain.Order, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() { o, e := b.Confirm(context.Background(), q.ID); results <- o; errs <- e }()
	}
	first, second := <-results, <-results
	for range 2 {
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
	}
	if first.ID != second.ID {
		t.Fatal("duplicate order")
	}
	var remaining int
	if err := db.QueryRow("SELECT remaining FROM events").Scan(&remaining); err != nil || remaining != 99 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
}
