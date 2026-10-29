package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"sqoch-ticket/backend/internal/dispatch"
	"time"
)

var ErrMockTemporary = errors.New("моковый отправитель временно недоступен")
var ErrMockResponseLost = errors.New("ответ мокового отправителя потерян; результат будет проверен")

// MockSender commits acceptance in a separate database, outside the job/result
// transaction. This preserves the real-world failure window for recovery tests.
type MockSender struct {
	DB    *sql.DB
	Mode  string
	Delay time.Duration
}

func (s *MockSender) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mock_acceptances(id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,email TEXT NOT NULL,subject TEXT NOT NULL,body TEXT NOT NULL,accepted_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS mock_calls(id TEXT PRIMARY KEY,calls INTEGER NOT NULL);`)
	return err
}
func (s *MockSender) Lookup(ctx context.Context, c dispatch.Command) (bool, error) {
	var fingerprint string
	err := s.DB.QueryRowContext(ctx, "SELECT fingerprint FROM mock_acceptances WHERE id=?", c.ID).Scan(&fingerprint)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fingerprint != c.Fingerprint() {
		return false, dispatch.ErrPoison
	}
	return true, nil
}
func (s *MockSender) Send(ctx context.Context, c dispatch.Command) error {
	timer := time.NewTimer(s.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fingerprint string
	err = tx.QueryRowContext(ctx, "SELECT fingerprint FROM mock_acceptances WHERE id=?", c.ID).Scan(&fingerprint)
	if err == nil {
		if fingerprint != c.Fingerprint() {
			return dispatch.ErrPoison
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var calls int
	err = tx.QueryRowContext(ctx, "INSERT INTO mock_calls(id,calls) VALUES(?,1) ON CONFLICT(id) DO UPDATE SET calls=calls+1 RETURNING calls", c.ID).Scan(&calls)
	if err != nil {
		return err
	}
	if s.Mode == "always_fail" || (s.Mode == "temporary_once" && calls == 1) {
		if err = tx.Commit(); err != nil {
			return err
		}
		return ErrMockTemporary
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO mock_acceptances(id,fingerprint,email,subject,body,accepted_at) VALUES(?,?,?,?,?,?)", c.ID, c.Fingerprint(), c.Email, c.Subject, c.Body, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.Mode == "after_accept_once" && calls == 1 {
		return ErrMockResponseLost
	}
	return nil
}
