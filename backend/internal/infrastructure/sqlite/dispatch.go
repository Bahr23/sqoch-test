package sqlite

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/outbox"
	"strings"
	"time"
)

func migrateDispatch(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 4 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
 ALTER TABLE mailings RENAME TO mailings_v3;
 CREATE TABLE mailings(
 id TEXT PRIMARY KEY,event_id TEXT NOT NULL REFERENCES events(id),body TEXT NOT NULL CHECK(length(body)<=10000),
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','running','completed','completed_with_errors')),
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL,confirmed_at TEXT NOT NULL DEFAULT '',event_title_snapshot TEXT NOT NULL DEFAULT '');
 INSERT INTO mailings(id,event_id,body,status,created_at,updated_at) SELECT id,event_id,body,status,created_at,updated_at FROM mailings_v3;
 DROP TABLE mailings_v3;
 CREATE INDEX mailings_event ON mailings(event_id);
 CREATE TABLE mailing_deliveries(
 id TEXT PRIMARY KEY,mailing_id TEXT NOT NULL REFERENCES mailings(id),email TEXT NOT NULL,names TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','processing','retry_wait','sent','skipped','failed')),
 generation INTEGER NOT NULL DEFAULT 0,result_version INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL,
 UNIQUE(mailing_id,email));
 CREATE INDEX mailing_deliveries_parent ON mailing_deliveries(mailing_id);
 `+outbox.Schema+` PRAGMA user_version=4;`)
	return err
}
func audienceHash(recipients []domain.MailingRecipient) string {
	data, _ := json.Marshal(recipients)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func dispatchDetails(ctx context.Context, tx *sql.Tx, d domain.MailingDetails) (domain.MailingDetails, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,email,names,status,attempts,error,updated_at FROM mailing_deliveries WHERE mailing_id=? ORDER BY email", d.ID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	d.Progress = &domain.MailingProgress{}
	d.Deliveries = []domain.MailingDelivery{}
	for rows.Next() {
		var v domain.MailingDelivery
		var names string
		if err = rows.Scan(&v.ID, &v.Email, &names, &v.Status, &v.Attempts, &v.Error, &v.UpdatedAt); err != nil {
			return d, err
		}
		if err = json.Unmarshal([]byte(names), &v.Names); err != nil {
			return d, err
		}
		d.Deliveries = append(d.Deliveries, v)
		d.Recipients = append(d.Recipients, domain.MailingRecipient{Email: v.Email, Names: v.Names})
		d.Progress.Total++
		switch v.Status {
		case "pending":
			d.Progress.Pending++
		case "processing":
			d.Progress.Processing++
		case "retry_wait":
			d.Progress.RetryWait++
		case "sent":
			d.Progress.Sent++
		case "skipped":
			d.Progress.Skipped++
		case "failed":
			d.Progress.Failed++
		}
	}
	return d, rows.Err()
}
func (r *MailingRepository) ConfirmMailing(ctx context.Context, id string, confirmation domain.MailingConfirmation) (domain.MailingDetails, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MailingDetails{}, err
	}
	defer tx.Rollback()
	m, err := mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	if m.Status != "draft" {
		return m, nil
	}
	if !m.EventAvailable {
		return m, &domain.ValidationError{Fields: map[string]string{"event_id": "Ивент недоступен."}}
	}
	if m.UpdatedAt != confirmation.ExpectedUpdatedAt || subtle.ConstantTimeCompare([]byte(m.AudienceHash), []byte(confirmation.AudienceHash)) != 1 {
		return m, domain.ErrMailingChanged
	}
	if strings.TrimSpace(m.Body) == "" {
		return m, &domain.ValidationError{Fields: map[string]string{"body": "Добавьте текст сообщения перед запуском."}}
	}
	if len(m.Recipients) == 0 {
		return m, &domain.ValidationError{Fields: map[string]string{"recipients": "Нет получателей с действующими покупками."}}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "UPDATE mailings SET status='running',confirmed_at=?,updated_at=?,event_title_snapshot=? WHERE id=? AND status='draft'", now, now, m.EventTitle, id); err != nil {
		return m, err
	}
	for _, recipient := range m.Recipients {
		jobID := uuid.NewString()
		names, _ := json.Marshal(recipient.Names)
		if _, err = tx.ExecContext(ctx, "INSERT INTO mailing_deliveries(id,mailing_id,email,names,status,updated_at) VALUES(?,?,?,?,'pending',?)", jobID, id, recipient.Email, string(names), now); err != nil {
			return m, err
		}
		cmd := dispatch.Command{Schema: 1, ID: jobID, MailingID: id, EventID: m.EventID, Email: recipient.Email, Subject: m.EventTitle, Body: m.Body}
		if err = outbox.Add(ctx, tx, r.commandsQueue, jobID+":0", cmd); err != nil {
			return m, err
		}
	}
	m, err = mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}
func (r *MailingRepository) ResumeMailing(ctx context.Context, id string) (domain.MailingDetails, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MailingDetails{}, err
	}
	defer tx.Rollback()
	m, err := mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	if m.Status == "draft" {
		return m, &domain.ValidationError{Fields: map[string]string{"mailing": "Сначала подтвердите запуск рассылки."}}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	changed := false
	for _, d := range m.Deliveries {
		if d.Status != "failed" {
			continue
		}
		changed = true
		var generation int
		if err = tx.QueryRowContext(ctx, "UPDATE mailing_deliveries SET status='pending',generation=generation+1,result_version=0,error='',updated_at=? WHERE id=? AND status='failed' RETURNING generation", now, d.ID).Scan(&generation); err != nil {
			return m, err
		}
		cmd := dispatch.Command{Schema: 1, ID: d.ID, MailingID: id, EventID: m.EventID, Email: d.Email, Subject: m.EventTitle, Body: m.Body, Generation: generation}
		if err = outbox.Add(ctx, tx, r.commandsQueue, fmt.Sprintf("%s:%d", d.ID, generation), cmd); err != nil {
			return m, err
		}
	}
	if changed {
		if _, err = tx.ExecContext(ctx, "UPDATE mailings SET status='running',updated_at=? WHERE id=?", now, id); err != nil {
			return m, err
		}
	}
	m, err = mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}
func (r *MailingRepository) ApplyResult(ctx context.Context, payload []byte) error {
	var result dispatch.Result
	if json.Unmarshal(payload, &result) != nil || !result.Valid() {
		return dispatch.ErrPoison
	}
	if _, err := time.Parse(time.RFC3339Nano, result.UpdatedAt); err != nil {
		return dispatch.ErrPoison
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var mailingID, status string
	var generation, version int
	if err = tx.QueryRowContext(ctx, "SELECT mailing_id,status,generation,result_version FROM mailing_deliveries WHERE id=?", result.ID).Scan(&mailingID, &status, &generation, &version); err == sql.ErrNoRows {
		return dispatch.ErrPoison
	} else if err != nil {
		return err
	}
	if result.Generation != generation || result.Version <= version || status == "sent" || status == "skipped" {
		return nil
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mailing_deliveries SET status=?,result_version=?,attempts=?,error=?,updated_at=? WHERE id=?", result.Status, result.Version, result.Attempts, result.Error, result.UpdatedAt, result.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE mailings SET status=CASE
 WHEN EXISTS(SELECT 1 FROM mailing_deliveries WHERE mailing_id=? AND status IN ('pending','processing','retry_wait')) THEN 'running'
 WHEN EXISTS(SELECT 1 FROM mailing_deliveries WHERE mailing_id=? AND status='failed') THEN 'completed_with_errors'
 ELSE 'completed' END,updated_at=? WHERE id=?`, mailingID, mailingID, time.Now().UTC().Format(time.RFC3339Nano), mailingID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *MailingRepository) Eligible(ctx context.Context, id string) (bool, error) {
	var eventID, email string
	err := r.db.QueryRowContext(ctx, "SELECT m.event_id,d.email FROM mailing_deliveries d JOIN mailings m ON m.id=d.mailing_id WHERE d.id=?", id).Scan(&eventID, &email)
	if err == sql.ErrNoRows {
		return false, domain.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var available bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM events WHERE id=? AND deleted_at IS NULL)", eventID).Scan(&available); err != nil || !available {
		return false, err
	}
	recipients, err := mailingRecipients(ctx, tx, eventID)
	if err != nil {
		return false, err
	}
	for _, recipient := range recipients {
		if recipient.Email == email {
			return true, nil
		}
	}
	return false, nil
}
