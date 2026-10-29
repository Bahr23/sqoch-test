package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/infrastructure/outbox"
	"sqoch-ticket/backend/internal/notifications/domain"
	"time"
)

type Store struct {
	DB          *sql.DB
	ResultQueue string
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS notification_jobs(
 id TEXT PRIMARY KEY,mailing_id TEXT NOT NULL,email TEXT NOT NULL,command TEXT NOT NULL,fingerprint TEXT NOT NULL,
 generation INTEGER NOT NULL,status TEXT NOT NULL CHECK(status IN ('pending','processing','retry_wait','sent','skipped','failed')),
 version INTEGER NOT NULL DEFAULT 0,attempts INTEGER NOT NULL DEFAULT 0,cycle_attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt INTEGER NOT NULL DEFAULT 0,lease_until INTEGER NOT NULL DEFAULT 0,lease_token TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL,UNIQUE(mailing_id,email));
 CREATE INDEX IF NOT EXISTS notification_due ON notification_jobs(status,next_attempt,lease_until);
 `+outbox.Schema)
	return err
}
func (s *Store) Ingest(ctx context.Context, payload []byte) error {
	var c dispatch.Command
	if json.Unmarshal(payload, &c) != nil || !c.Valid() {
		return dispatch.ErrPoison
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var fingerprint, status string
	var generation int
	err = tx.QueryRowContext(ctx, "SELECT fingerprint,status,generation FROM notification_jobs WHERE id=?", c.ID).Scan(&fingerprint, &status, &generation)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err == sql.ErrNoRows {
		// A mailing/email may only have one stable delivery identity.
		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM notification_jobs WHERE mailing_id=? AND email=?)", c.MailingID, c.Email).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return dispatch.ErrPoison
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO notification_jobs(id,mailing_id,email,command,fingerprint,generation,status,updated_at) VALUES(?,?,?,?,?,?,'pending',?)", c.ID, c.MailingID, c.Email, string(payload), c.Fingerprint(), c.Generation, now)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if fingerprint != c.Fingerprint() {
		return dispatch.ErrPoison
	}
	if c.Generation <= generation {
		return nil
	}
	if status != "sent" && status != "skipped" {
		status = "pending"
	}
	_, err = tx.ExecContext(ctx, "UPDATE notification_jobs SET command=?,generation=?,status=?,cycle_attempts=0,next_attempt=0,lease_until=0,lease_token='',error='',version=version+1,updated_at=? WHERE id=?", string(payload), c.Generation, status, now, c.ID)
	if err != nil {
		return err
	}
	if err = s.emit(ctx, tx, c.ID); err != nil {
		return err
	}
	return tx.Commit()
}

const jobColumns = "command,status,version,attempts,cycle_attempts,lease_token,error,updated_at"

func scanJob(row interface{ Scan(...any) error }) (domain.Job, error) {
	var j domain.Job
	var command string
	err := row.Scan(&command, &j.Status, &j.Version, &j.Attempts, &j.CycleAttempts, &j.LeaseToken, &j.Error, &j.UpdatedAt)
	if err != nil {
		return j, err
	}
	err = json.Unmarshal([]byte(command), &j.Command)
	return j, err
}
func (s *Store) emit(ctx context.Context, tx *sql.Tx, id string) error {
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM notification_jobs WHERE id=?", id))
	if err != nil {
		return err
	}
	r := dispatch.Result{Schema: 1, ID: id, Generation: j.Command.Generation, Version: j.Version, Status: j.Status, Attempts: j.Attempts, Error: j.Error, UpdatedAt: j.UpdatedAt}
	return outbox.Add(ctx, tx, s.ResultQueue, fmt.Sprintf("%s:%d:%d", id, r.Generation, r.Version), r)
}
func (s *Store) Claim(ctx context.Context, now time.Time, lease time.Duration) (domain.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM notification_jobs WHERE
 (status IN ('pending','retry_wait') AND next_attempt<=?) OR (status='processing' AND lease_until<=?)
 ORDER BY next_attempt,id LIMIT 1`, now.UnixMilli(), now.UnixMilli()).Scan(&id)
	if err != nil {
		return domain.Job{}, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE notification_jobs SET status='processing',attempts=attempts+1,cycle_attempts=cycle_attempts+1,lease_token=?,lease_until=?,version=version+1,updated_at=? WHERE id=?", uuid.NewString(), now.Add(lease).UnixMilli(), now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return domain.Job{}, err
	}
	if err = s.emit(ctx, tx, id); err != nil {
		return domain.Job{}, err
	}
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM notification_jobs WHERE id=?", id))
	if err != nil {
		return j, err
	}
	return j, tx.Commit()
}
func (s *Store) Finish(ctx context.Context, j domain.Job, status, message string, next time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE notification_jobs SET status=?,error=?,next_attempt=?,lease_until=0,lease_token='',version=version+1,updated_at=?
 WHERE id=? AND generation=? AND lease_token=? AND status='processing'`, status, message, next.UnixMilli(), time.Now().UTC().Format(time.RFC3339Nano), j.Command.ID, j.Command.Generation, j.LeaseToken)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if err = s.emit(ctx, tx, j.Command.ID); err != nil {
		return err
	}
	return tx.Commit()
}
