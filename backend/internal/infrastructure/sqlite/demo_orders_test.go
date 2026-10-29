package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"testing"
	"time"
)

func TestDemoOrdersPreservePurchasesAndSeedOnce(t *testing.T) {
	db, booking, repo := setup(t)
	ctx := context.Background()
	quote := draft(t, booking, false)
	original, err := booking.Confirm(ctx, quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	for run := range 2 {
		report, err := sqlite.SeedDemoOrders(ctx, db, now)
		if err != nil {
			t.Fatal(err)
		}
		if report.Created != 20*(1-run) || report.Existing != 20*run || len(report.Events) != 2 {
			t.Fatalf("run %d: %+v", run, report)
		}
		for _, e := range report.Events {
			if e.Active != 7 || e.Cancelled != 3 || e.ActiveTickets != 14 {
				t.Fatalf("unbalanced fixture: %+v", e)
			}
		}
		first, _ := repo.Event(ctx, sqlite.FirstEventID)
		second, _ := repo.Event(ctx, sqlite.SecondEventID)
		if first.Remaining != 85 || second.Remaining != 46 {
			t.Fatalf("incorrect inventory: %d / %d", first.Remaining, second.Remaining)
		}
		if err := sqlite.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	repeated, err := booking.Confirm(ctx, quote.ID)
	if err != nil || repeated.ID != original.ID || repeated.Status != domain.OrderActive {
		t.Fatalf("existing purchase damaged: %+v %v", repeated, err)
	}
	if _, err := booking.Confirm(ctx, "demo-orders-v1-1-03-checkout"); !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("cancelled purchase reconfirmed: %v", err)
	}
	active, err := booking.Confirm(ctx, "demo-orders-v1-2-01-checkout")
	if err != nil || active.Status != domain.OrderActive || active.Total != 2500 {
		t.Fatalf("seeded purchase cannot be retrieved: %+v %v", active, err)
	}
	var total, emails, crossEventEmails int
	if err := db.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT c.email) FROM orders o JOIN checkouts c ON c.id=o.checkout_id WHERE o.id LIKE 'demo-orders-v1-%'`).Scan(&total, &emails); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT c.email FROM orders o JOIN checkouts c ON c.id=o.checkout_id WHERE o.id LIKE 'demo-orders-v1-%' GROUP BY c.email HAVING COUNT(DISTINCT c.event_id)=2)`).Scan(&crossEventEmails); err != nil {
		t.Fatal(err)
	}
	if total != 20 || emails != 6 || crossEventEmails != 6 {
		t.Fatalf("bad email distribution: %d %d %d", total, emails, crossEventEmails)
	}
	rows, err := db.Query(`SELECT c.email,c.participants,c.total,c.event_snapshot,o.status,o.created_at,COALESCE(o.cancelled_at,'') FROM orders o JOIN checkouts c ON c.id=o.checkout_id WHERE o.id LIKE 'demo-orders-v1-%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var email, peopleJSON, eventJSON, status, createdAt, cancelledAt string
		var total int
		if err := rows.Scan(&email, &peopleJSON, &total, &eventJSON, &status, &createdAt, &cancelledAt); err != nil {
			t.Fatal(err)
		}
		var people []domain.Participant
		var event domain.Event
		if err := json.Unmarshal([]byte(peopleJSON), &people); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			t.Fatal(err)
		}
		input := domain.OrderInput{Email: email}
		for _, p := range people {
			input.Participants = append(input.Participants, domain.ParticipantInput{Name: p.Name, Age: &p.Age})
		}
		priced, expectedTotal, err := domain.PriceOrder(input, event)
		if err != nil || expectedTotal != total {
			t.Fatalf("invalid fixture purchase: %s %v", peopleJSON, err)
		}
		for i := range people {
			if priced[i] != people[i] {
				t.Fatal("ticket price/category mismatch")
			}
		}
		created, err := time.Parse(time.RFC3339, createdAt)
		if err != nil || !created.Before(now) {
			t.Fatal("invalid purchase date")
		}
		if status == domain.OrderCancelled {
			cancelled, err := time.Parse(time.RFC3339, cancelledAt)
			if err != nil || !cancelled.After(created) || !cancelled.Before(now) {
				t.Fatal("invalid cancellation date")
			}
		} else if cancelledAt != "" {
			t.Fatal("active order has cancellation date")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestDemoOrdersRollbackWholeBatchWhenSeatsUnavailable(t *testing.T) {
	db, _, repo := setup(t)
	ctx := context.Background()
	if _, err := db.Exec("UPDATE events SET remaining=0 WHERE id=?", sqlite.SecondEventID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.SeedDemoOrders(ctx, db, time.Now()); !errors.Is(err, domain.ErrSoldOut) {
		t.Fatalf("expected stock failure: %v", err)
	}
	var orders, checkouts int
	if err := db.QueryRow("SELECT (SELECT COUNT(*) FROM orders),(SELECT COUNT(*) FROM checkouts)").Scan(&orders, &checkouts); err != nil {
		t.Fatal(err)
	}
	first, _ := repo.Event(ctx, sqlite.FirstEventID)
	if orders != 0 || checkouts != 0 || first.Remaining != 100 {
		t.Fatalf("partial fixture committed: %d %d %d", orders, checkouts, first.Remaining)
	}
}
