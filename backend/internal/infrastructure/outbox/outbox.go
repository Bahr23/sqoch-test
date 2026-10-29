package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const Schema = `CREATE TABLE IF NOT EXISTS message_outbox (
 id TEXT PRIMARY KEY, queue TEXT NOT NULL, payload BLOB NOT NULL,
 created_at INTEGER NOT NULL, published_at INTEGER);
 CREATE INDEX IF NOT EXISTS outbox_pending ON message_outbox(published_at,created_at);`

type Message struct {
	ID, Queue string
	Payload   []byte
}

func Add(ctx context.Context, tx *sql.Tx, queue, id string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO message_outbox(id,queue,payload,created_at) VALUES(?,?,?,?)", id, queue, payload, time.Now().UnixMilli())
	return err
}

type Store struct{ DB *sql.DB }

func (s Store) Next(ctx context.Context) (Message, error) {
	var m Message
	err := s.DB.QueryRowContext(ctx, "SELECT id,queue,payload FROM message_outbox WHERE published_at IS NULL ORDER BY created_at,id LIMIT 1").Scan(&m.ID, &m.Queue, &m.Payload)
	return m, err
}
func (s Store) Published(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE message_outbox SET published_at=? WHERE id=?", time.Now().UnixMilli(), id)
	return err
}
