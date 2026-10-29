package sqlite

import (
	"context"
	"database/sql"
)

func migrateOrderStatuses(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 2 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
 ALTER TABLE orders ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','cancelled'));
 ALTER TABLE orders ADD COLUMN cancelled_at TEXT CHECK(
   (status='active' AND cancelled_at IS NULL) OR
   (status='cancelled' AND cancelled_at IS NOT NULL)
 );
 CREATE INDEX orders_status ON orders(status);
 PRAGMA user_version=2;
 `)
	return err
}
