package sqlite

import (
	"context"
	"database/sql"
	"sqoch-ticket/backend/internal/usecase"
	"time"
)

type AdminSessions struct{ db *sql.DB }

func NewAdminSessions(ctx context.Context, db *sql.DB) (*AdminSessions, error) {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS admin_sessions (id TEXT PRIMARY KEY, email TEXT NOT NULL, expires_at INTEGER NOT NULL)`)
	return &AdminSessions{db: db}, err
}
func (s *AdminSessions) Create(ctx context.Context, session usecase.AdminSession) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_sessions (id,email,expires_at) VALUES (?,?,?)`, session.ID, session.Email, session.ExpiresAt.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *AdminSessions) Active(ctx context.Context, session usecase.AdminSession) (bool, error) {
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_sessions WHERE id=? AND email=? AND expires_at=? AND expires_at>?)`, session.ID, session.Email, session.ExpiresAt.Unix(), time.Now().Unix()).Scan(&active)
	return active, err
}
func (s *AdminSessions) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id=?`, id)
	return err
}
