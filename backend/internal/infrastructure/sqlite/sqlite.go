package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize sqlite: %w", err)
	}
	return db, nil
}
