package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sqoch-ticket/backend/internal/domain"
	"strings"
	"time"
)

func migrateMailings(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 3 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE mailings (
  id TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  body TEXT NOT NULL CHECK(length(body)<=10000),
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status='draft'),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
 );
 CREATE INDEX mailings_event ON mailings(event_id);
 PRAGMA user_version=3;
 `)
	return err
}

type MailingRepository struct {
	db            *sql.DB
	commandsQueue string
}

func NewMailingRepository(db *sql.DB, queue ...string) *MailingRepository {
	q := "sqoch.notifications.commands.v1"
	if len(queue) > 0 {
		q = queue[0]
	}
	return &MailingRepository{db: db, commandsQueue: q}
}

const mailingSelect = `SELECT m.id,m.event_id,CASE WHEN m.status='draft' THEN e.title ELSE m.event_title_snapshot END,e.deleted_at IS NULL,m.body,m.status,m.created_at,m.updated_at,m.confirmed_at FROM mailings m JOIN events e ON e.id=m.event_id`

func scanMailing(row scanner) (domain.Mailing, error) {
	var m domain.Mailing
	err := row.Scan(&m.ID, &m.EventID, &m.EventTitle, &m.EventAvailable, &m.Body, &m.Status, &m.CreatedAt, &m.UpdatedAt, &m.ConfirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return m, err
}

func (r *MailingRepository) Mailings(ctx context.Context) ([]domain.Mailing, error) {
	rows, err := r.db.QueryContext(ctx, mailingSelect+" ORDER BY m.updated_at DESC,m.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.Mailing{}
	for rows.Next() {
		m, err := scanMailing(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func mailingDetails(ctx context.Context, tx *sql.Tx, id string) (domain.MailingDetails, error) {
	m, err := scanMailing(tx.QueryRowContext(ctx, mailingSelect+" WHERE m.id=?", id))
	if err != nil {
		return domain.MailingDetails{}, err
	}
	details := domain.MailingDetails{Mailing: m, Recipients: []domain.MailingRecipient{}}
	if m.Status != "draft" {
		return dispatchDetails(ctx, tx, details)
	}
	if m.EventAvailable {
		details.Recipients, err = mailingRecipients(ctx, tx, m.EventID)
	}
	details.AudienceHash = audienceHash(details.Recipients)
	return details, err
}

func (r *MailingRepository) Mailing(ctx context.Context, id string) (domain.MailingDetails, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MailingDetails{}, err
	}
	defer tx.Rollback()
	m, err := mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}

func (r *MailingRepository) SaveMailing(ctx context.Context, id string, input domain.MailingInput, create bool) (domain.MailingDetails, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MailingDetails{}, err
	}
	defer tx.Rollback()
	if !create {
		current, err := scanMailing(tx.QueryRowContext(ctx, mailingSelect+" WHERE m.id=?", id))
		if err != nil {
			return domain.MailingDetails{}, err
		}
		if current.Status != "draft" {
			return domain.MailingDetails{}, domain.ErrMailingLocked
		}
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM events WHERE id=? AND deleted_at IS NULL)", input.EventID).Scan(&exists); err != nil {
		return domain.MailingDetails{}, err
	}
	if !exists {
		return domain.MailingDetails{}, &domain.ValidationError{Fields: map[string]string{"event_id": "Ивент недоступен. Выберите другой ивент."}}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO mailings(id,event_id,body,created_at,updated_at) VALUES(?,?,?,?,?)", id, input.EventID, input.Body, now, now)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE mailings SET event_id=?,body=?,updated_at=? WHERE id=? AND status='draft'", input.EventID, input.Body, now, id)
		if err == nil {
			var n int64
			n, err = result.RowsAffected()
			if err == nil && n == 0 {
				return domain.MailingDetails{}, domain.ErrNotFound
			}
		}
	}
	if err != nil {
		return domain.MailingDetails{}, err
	}
	m, err := mailingDetails(ctx, tx, id)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}

func (r *MailingRepository) MailingRecipients(ctx context.Context, eventID string) ([]domain.MailingRecipient, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM events WHERE id=? AND deleted_at IS NULL)", eventID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	recipients, err := mailingRecipients(ctx, tx, eventID)
	if err != nil {
		return nil, err
	}
	return recipients, tx.Commit()
}

// Recompute the audience from active purchases whenever a draft is opened or
// saved. Drafts do not freeze contact data or revive cancelled purchases.
func mailingRecipients(ctx context.Context, tx *sql.Tx, eventID string) ([]domain.MailingRecipient, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.email,c.participants FROM orders o JOIN checkouts c ON c.id=o.checkout_id WHERE c.event_id=? AND o.status='active' ORDER BY o.created_at,o.id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byEmail := map[string]*domain.MailingRecipient{}
	for rows.Next() {
		var email, peopleJSON string
		if err := rows.Scan(&email, &peopleJSON); err != nil {
			return nil, err
		}
		email = strings.ToLower(strings.TrimSpace(email))
		var people []domain.Participant
		if err := json.Unmarshal([]byte(peopleJSON), &people); err != nil {
			return nil, err
		}
		recipient := byEmail[email]
		if recipient == nil {
			recipient = &domain.MailingRecipient{Email: email, Names: []string{}}
			byEmail[email] = recipient
		}
		recipient.OrderCount++
		seen := map[string]bool{}
		for _, name := range recipient.Names {
			seen[name] = true
		}
		for _, p := range people {
			name := strings.TrimSpace(p.Name)
			if name != "" && !seen[name] {
				recipient.Names = append(recipient.Names, name)
				seen[name] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list := make([]domain.MailingRecipient, 0, len(byEmail))
	for _, r := range byEmail {
		list = append(list, *r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Email < list[j].Email })
	return list, nil
}
