package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sqoch-ticket/backend/internal/domain"
	"time"
)

type AssistantRepository struct{ db *sql.DB }

func NewAssistantRepository(db *sql.DB) *AssistantRepository { return &AssistantRepository{db} }
func migrateAssistant(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 5 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE chat_threads(id TEXT PRIMARY KEY,owner TEXT NOT NULL,title TEXT NOT NULL,updated_at TEXT NOT NULL);
 CREATE INDEX chat_owner ON chat_threads(owner,updated_at);
 CREATE TABLE chat_runs(id TEXT PRIMARY KEY,thread_id TEXT NOT NULL REFERENCES chat_threads(id),input TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('queued','running','completed','failed')),error TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL);
 CREATE UNIQUE INDEX chat_active_run ON chat_runs(thread_id) WHERE status IN ('queued','running','failed');
 CREATE TABLE chat_messages(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,thread_id TEXT NOT NULL REFERENCES chat_threads(id),run_id TEXT NOT NULL REFERENCES chat_runs(id),payload TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE INDEX chat_history ON chat_messages(thread_id,seq);
 CREATE TABLE chat_proposals(id TEXT PRIMARY KEY,thread_id TEXT NOT NULL REFERENCES chat_threads(id),run_id TEXT NOT NULL UNIQUE REFERENCES chat_runs(id),payload TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('pending','applied','rejected')),mailing_id TEXT NOT NULL DEFAULT '');
 PRAGMA user_version=5;`)
	return err
}
func chatNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func owned(ctx context.Context, tx *sql.Tx, owner, id string) error {
	var ok bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM chat_threads WHERE id=? AND owner=?)", id, owner).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return domain.ErrNotFound
	}
	return nil
}
func (r *AssistantRepository) Threads(ctx context.Context, owner string) ([]domain.ChatThread, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,title,updated_at FROM chat_threads WHERE owner=? ORDER BY updated_at DESC LIMIT 100", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.ChatThread{}
	for rows.Next() {
		var t domain.ChatThread
		if err := rows.Scan(&t.ID, &t.Title, &t.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
func (r *AssistantRepository) CreateThread(ctx context.Context, owner, id string) (domain.ChatThread, error) {
	t := domain.ChatThread{ID: id, Title: "Новый диалог", UpdatedAt: chatNow()}
	_, err := r.db.ExecContext(ctx, "INSERT INTO chat_threads(id,owner,title,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", id, owner, t.Title, t.UpdatedAt)
	if err != nil {
		return t, err
	}
	err = r.db.QueryRowContext(ctx, "SELECT id,title,updated_at FROM chat_threads WHERE id=? AND owner=?", id, owner).Scan(&t.ID, &t.Title, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return t, err
}
func (r *AssistantRepository) Details(ctx context.Context, owner, id string) (domain.ChatDetails, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ChatDetails{}, err
	}
	defer tx.Rollback()
	d := domain.ChatDetails{Messages: []domain.ChatMessage{}, Proposals: []domain.Proposal{}, Runs: []domain.ChatRun{}}
	if err = owned(ctx, tx, owner, id); err != nil {
		return d, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT id,title,updated_at FROM chat_threads WHERE id=?", id).Scan(&d.ID, &d.Title, &d.UpdatedAt); err != nil {
		return d, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,run_id,payload,created_at FROM chat_messages WHERE thread_id=? ORDER BY seq", id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var m domain.ChatMessage
		var b string
		if err = rows.Scan(&m.ID, &m.RunID, &b, &m.CreatedAt); err == nil {
			err = json.Unmarshal([]byte(b), &m.AIMessage)
		}
		if err != nil {
			rows.Close()
			return d, err
		}
		d.Messages = append(d.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT payload,status,mailing_id FROM chat_proposals WHERE thread_id=? ORDER BY rowid", id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		p, e := scanProposal(rows)
		if e != nil {
			rows.Close()
			return d, e
		}
		d.Proposals = append(d.Proposals, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,thread_id,status,error FROM chat_runs WHERE thread_id=? ORDER BY rowid", id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var run domain.ChatRun
		if err = rows.Scan(&run.ID, &run.ThreadID, &run.Status, &run.Error); err != nil {
			rows.Close()
			return d, err
		}
		d.Runs = append(d.Runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}
func insertChatMessage(ctx context.Context, tx *sql.Tx, thread, run, id string, m domain.AIMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO chat_messages(id,thread_id,run_id,payload,created_at) VALUES(?,?,?,?,?)", id, thread, run, string(b), chatNow())
	return err
}
func (r *AssistantRepository) Enqueue(ctx context.Context, owner, thread, id, input string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = owned(ctx, tx, owner, thread); err != nil {
		return err
	}
	var oldThread, oldInput string
	err = tx.QueryRowContext(ctx, "SELECT thread_id,input FROM chat_runs WHERE id=?", id).Scan(&oldThread, &oldInput)
	if err == nil {
		if oldThread == thread && oldInput == input {
			return nil
		}
		return domain.ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var active, count, size int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_runs WHERE thread_id=? AND status!='completed'", thread).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return domain.ErrChatBusy
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(length(payload)),0) FROM chat_messages WHERE thread_id=?", thread).Scan(&count, &size); err != nil {
		return err
	}
	if count >= 150 || size > 120000 {
		return domain.ErrChatLimit
	}
	now := chatNow()
	if _, err = tx.ExecContext(ctx, "INSERT INTO chat_runs(id,thread_id,input,status,created_at) VALUES(?,?,?,'queued',?)", id, thread, input, now); err != nil {
		return err
	}
	if err = insertChatMessage(ctx, tx, thread, id, uuid.NewString(), domain.AIMessage{Role: "user", Content: input}); err != nil {
		return err
	}
	title := []rune(input)
	if len(title) > 60 {
		title = append(title[:60], '…')
	}
	if _, err = tx.ExecContext(ctx, "UPDATE chat_threads SET title=CASE WHEN title='Новый диалог' THEN ? ELSE title END,updated_at=? WHERE id=?", string(title), now, thread); err != nil {
		return err
	}
	return tx.Commit()
}

// One worker in one API replica. A restart recovers its persisted unfinished run.
func (r *AssistantRepository) Recover(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "UPDATE chat_runs SET status='queued' WHERE status='running'")
	return err
}
func (r *AssistantRepository) Next(ctx context.Context) (domain.ChatRun, string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ChatRun{}, "", err
	}
	defer tx.Rollback()
	var run domain.ChatRun
	var owner string
	err = tx.QueryRowContext(ctx, "SELECT r.id,r.thread_id,r.status,r.error,t.owner FROM chat_runs r JOIN chat_threads t ON t.id=r.thread_id WHERE r.status='queued' ORDER BY r.rowid LIMIT 1").Scan(&run.ID, &run.ThreadID, &run.Status, &run.Error, &owner)
	if err != nil {
		return run, owner, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE chat_runs SET status='running',attempts=attempts+1 WHERE id=?", run.ID)
	if err != nil {
		return run, owner, err
	}
	return run, owner, tx.Commit()
}
func (r *AssistantRepository) Append(ctx context.Context, run domain.ChatRun, m domain.AIMessage, done bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = insertChatMessage(ctx, tx, run.ThreadID, run.ID, uuid.NewString(), m); err != nil {
		return err
	}
	if done {
		if _, err = tx.ExecContext(ctx, "UPDATE chat_runs SET status='completed',error='' WHERE id=?", run.ID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE chat_threads SET updated_at=? WHERE id=?", chatNow(), run.ThreadID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AssistantRepository) Fail(ctx context.Context, id, message string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE chat_runs SET status='failed',error=? WHERE id=?", message, id)
	return err
}
func (r *AssistantRepository) Retry(ctx context.Context, owner, thread, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = owned(ctx, tx, owner, thread); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM chat_runs WHERE id=? AND thread_id=?", id, thread).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "queued" || status == "running" || status == "completed" {
		return nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE chat_runs SET status='queued',error='' WHERE id=?", id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func scanProposal(s scanner) (domain.Proposal, error) {
	var p domain.Proposal
	var b, status, mailing string
	err := s.Scan(&b, &status, &mailing)
	if errors.Is(err, sql.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(b), &p); err != nil {
		return p, err
	}
	p.Status = status
	p.MailingID = mailing
	return p, nil
}
func (r *AssistantRepository) Propose(ctx context.Context, p domain.Proposal) (domain.Proposal, error) {
	// The run ID is the proposal ID: replay cannot create a second proposal.
	b, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, err = r.db.ExecContext(ctx, "INSERT INTO chat_proposals(id,thread_id,run_id,payload,status) VALUES(?,?,?,?,'pending') ON CONFLICT(run_id) DO NOTHING", p.ID, p.ThreadID, p.RunID, string(b))
	if err != nil {
		return p, err
	}
	return scanProposal(r.db.QueryRowContext(ctx, "SELECT payload,status,mailing_id FROM chat_proposals WHERE run_id=?", p.RunID))
}
func (r *AssistantRepository) Decide(ctx context.Context, owner, thread, id string, apply bool) (domain.Proposal, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Proposal{}, err
	}
	defer tx.Rollback()
	if err = owned(ctx, tx, owner, thread); err != nil {
		return domain.Proposal{}, err
	}
	p, err := scanProposal(tx.QueryRowContext(ctx, "SELECT payload,status,mailing_id FROM chat_proposals WHERE id=? AND thread_id=?", id, thread))
	if err != nil {
		return p, err
	}
	desired := "rejected"
	if apply {
		desired = "applied"
	}
	if p.Status == desired {
		return p, nil
	}
	if p.Status != "pending" {
		return p, domain.ErrConflict
	}
	// Decisions are serialized with the conversation, so the next turn sees them.
	var busy int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM chat_runs WHERE thread_id=? AND status!='completed'", thread).Scan(&busy); err != nil {
		return p, err
	}
	if busy > 0 {
		return p, domain.ErrChatBusy
	}
	if apply {
		current, e := scanEvent(tx.QueryRowContext(ctx, "SELECT "+eventColumns+" FROM events WHERE id=? AND deleted_at IS NULL", p.EventID))
		if e != nil {
			return p, e
		}
		if domain.EventFingerprint(current.Input()) != domain.EventFingerprint(p.Before) {
			return p, domain.ErrProposalStale
		}
		if err = p.After.Validate(); err != nil {
			return p, err
		}
		if p.After.Capacity < current.Sold {
			return p, domain.ErrConflict
		}
		v := p.After
		if _, err = tx.ExecContext(ctx, `UPDATE events SET title=?,description=?,category=?,tagline=?,cover_url=?,starts_at=?,timezone=?,venue=?,full_price=?,child_price=?,capacity=?,remaining=? WHERE id=?`, v.Title, v.Description, v.Category, v.Tagline, v.CoverURL, v.Date, v.Timezone, v.Venue, v.FullPrice, v.ChildPrice, v.Capacity, v.Capacity-current.Sold, p.EventID); err != nil {
			return p, err
		}
		if p.MailingBody != "" {
			p.MailingID = uuid.NewString()
			now := chatNow()
			if _, err = tx.ExecContext(ctx, "INSERT INTO mailings(id,event_id,body,created_at,updated_at) VALUES(?,?,?,?,?)", p.MailingID, p.EventID, p.MailingBody, now, now); err != nil {
				return p, err
			}
		}
	}
	p.Status = desired
	if _, err = tx.ExecContext(ctx, "UPDATE chat_proposals SET status=?,mailing_id=? WHERE id=?", p.Status, p.MailingID, id); err != nil {
		return p, err
	}
	note := "Организатор отклонил предложение " + id + ". Изменения не применены."
	if apply {
		note = "Организатор подтвердил предложение " + id + ". Изменения применены. ID черновика рассылки: " + p.MailingID + ". Отправка не запускалась."
	}
	if err = insertChatMessage(ctx, tx, thread, p.RunID, "decision:"+uuid.NewString(), domain.AIMessage{Role: "user", Content: note}); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
