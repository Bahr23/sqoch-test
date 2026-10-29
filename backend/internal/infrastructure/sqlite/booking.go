package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"sqoch-ticket/backend/internal/domain"
	"strings"
	"time"
)

//go:embed schema.sql
var schema string

func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if err := migrateEvents(ctx, tx); err != nil {
		return err
	}
	if err := migrateOrderStatuses(ctx, tx); err != nil {
		return err
	}
	if err := migrateMailings(ctx, tx); err != nil {
		return err
	}
	if err := migrateDispatch(ctx, tx); err != nil {
		return err
	}
	if err := migrateAssistant(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

type BookingRepository struct{ db *sql.DB }

func NewBookingRepository(db *sql.DB) *BookingRepository { return &BookingRepository{db: db} }
func (r *BookingRepository) CreateCheckout(ctx context.Context, c domain.Checkout, simulate bool) error {
	people, err := json.Marshal(c.Participants)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(c.Event)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO checkouts (id,event_id,email,participants,total,expires_at,simulate_sold_out,event_snapshot) SELECT ?,id,?,?,?,?,?,? FROM events WHERE id=? AND deleted_at IS NULL`, c.ID, c.Email, string(people), c.Total, c.ExpiresAt, simulate, string(snapshot), c.EventID)
	if err == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			return domain.ErrNotFound
		}
	}

	return err
}

// Confirm atomically claims seats and creates the order. Repeating the same
// checkout returns the original result, including after a lost HTTP response.
func (r *BookingRepository) Confirm(ctx context.Context, id string, now time.Time) (domain.Order, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback()
	var email, people, expires, eventID, snapshot string
	var total int
	var simulate bool
	err = tx.QueryRowContext(ctx, "SELECT email, participants, total, expires_at, simulate_sold_out, event_id, event_snapshot FROM checkouts WHERE id = ?", id).Scan(&email, &people, &total, &expires, &simulate, &eventID, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, err
	}
	order := domain.Order{EventID: eventID, Email: email, Total: total, Status: domain.OrderActive}
	if err := json.Unmarshal([]byte(snapshot), &order.Event); err != nil {
		return domain.Order{}, err
	}
	if err := json.Unmarshal([]byte(people), &order.Participants); err != nil {
		return domain.Order{}, err
	}
	err = tx.QueryRowContext(ctx, "SELECT id, number, created_at, status FROM orders WHERE checkout_id = ?", id).Scan(&order.ID, &order.Number, &order.CreatedAt, &order.Status)
	if err == nil {
		if order.Status == domain.OrderCancelled {
			return domain.Order{}, domain.ErrCancelled
		}
		return order, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Order{}, err
	}
	deadline, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return domain.Order{}, err
	}
	if !now.Before(deadline) {
		return domain.Order{}, domain.ErrExpired
	}
	// Demo-only stock race: fail this checkout without affecting other visitors.
	if simulate {
		return domain.Order{}, domain.ErrSoldOut
	}
	result, err := tx.ExecContext(ctx, "UPDATE events SET remaining = remaining - ? WHERE id = ? AND deleted_at IS NULL AND remaining >= ?", len(order.Participants), eventID, len(order.Participants))
	if err != nil {
		return domain.Order{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Order{}, err
	}
	if affected == 0 {
		return domain.Order{}, domain.ErrSoldOut
	}
	order.ID = rand.Text()
	order.Number = "SQ-" + strings.ToUpper(order.ID[:10])
	order.CreatedAt = now.Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, "INSERT INTO orders (id, checkout_id, number, created_at) VALUES (?, ?, ?, ?)", order.ID, id, order.Number, order.CreatedAt)
	if err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Order{}, err
	}
	return order, nil
}
