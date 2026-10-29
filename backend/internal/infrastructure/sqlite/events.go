package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sqoch-ticket/backend/internal/domain"
	"time"
	_ "time/tzdata"
)

const FirstEventID = "b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1"
const SecondEventID = "e6ab4d3b-df82-4dba-9444-1517059e038a"

func migrateEvents(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
 ALTER TABLE events ADD COLUMN description TEXT NOT NULL DEFAULT '';
 ALTER TABLE events ADD COLUMN category TEXT NOT NULL DEFAULT '';
 ALTER TABLE events ADD COLUMN tagline TEXT NOT NULL DEFAULT '';
 ALTER TABLE events ADD COLUMN cover_url TEXT NOT NULL DEFAULT '';
 ALTER TABLE events ADD COLUMN timezone TEXT NOT NULL DEFAULT 'Europe/Moscow';
 ALTER TABLE events ADD COLUMN full_price INTEGER NOT NULL DEFAULT 2000 CHECK(full_price BETWEEN 1 AND 1000000);
 ALTER TABLE events ADD COLUMN child_price INTEGER NOT NULL DEFAULT 1000 CHECK(child_price BETWEEN 1 AND 1000000);
 ALTER TABLE events ADD COLUMN capacity INTEGER NOT NULL DEFAULT 100 CHECK(capacity BETWEEN 1 AND 1000000);
 ALTER TABLE events ADD COLUMN deleted_at TEXT;
 ALTER TABLE checkouts ADD COLUMN event_snapshot TEXT NOT NULL DEFAULT '{}';
 CREATE INDEX IF NOT EXISTS checkouts_event ON checkouts(event_id);
 PRAGMA defer_foreign_keys=ON;
 `)
	if err != nil {
		return err
	}
	// Preserve the existing event, inventory, checkout links and orders.
	if _, err = tx.ExecContext(ctx, "UPDATE events SET id=? WHERE id='orbit'", FirstEventID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE checkouts SET event_id=? WHERE event_id='orbit'", FirstEventID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO events (id,title,starts_at,venue,remaining) VALUES (?, 'За пределами привычного','2026-11-21T18:00:00+03:00','Москва · Пространство «Орбита»',100)`, FirstEventID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE events SET description='Вечер открытий на стыке науки, музыки и искусства. Исследуйте новые идеи, встречайтесь с интересными людьми и выходите за рамки привычного.',category='НАУКА · МУЗЫКА · ОТКРЫТИЯ',tagline='ВЫЙТИ ЗА РАМКИ',capacity=remaining+COALESCE((SELECT SUM(json_array_length(c.participants)) FROM checkouts c JOIN orders o ON o.checkout_id=c.id WHERE c.event_id=events.id),0) WHERE id=?`, FirstEventID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events (id,title,starts_at,venue,remaining,capacity,full_price,child_price,description,category,tagline) VALUES (?, 'Город звука','2026-12-05T19:00:00+03:00','Санкт-Петербург · Студия «Волна»',60,60,2500,1200,'Живые выступления, электронная музыка и эксперименты со звуком. Вечер для тех, кто хочет услышать город по-новому.','МУЗЫКА · ЖИВОЙ ЗВУК','УСЛЫШАТЬ БОЛЬШЕ')`, SecondEventID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE checkouts SET event_snapshot=(SELECT json_object('id',e.id,'title',e.title,'date',e.starts_at,'venue',e.venue,'description',e.description,'category',e.category,'tagline',e.tagline,'cover_url',e.cover_url,'timezone',e.timezone,'full_price',e.full_price,'child_price',e.child_price,'remaining',e.remaining,'capacity',e.capacity,'sold',e.capacity-e.remaining) FROM events e WHERE e.id=checkouts.event_id); PRAGMA user_version=1;`)
	return err
}

const eventColumns = `id,title,starts_at,venue,remaining,description,category,tagline,cover_url,timezone,full_price,child_price,capacity`

type scanner interface{ Scan(...any) error }

func scanEvent(row scanner) (domain.Event, error) {
	var e domain.Event
	err := row.Scan(&e.ID, &e.Title, &e.Date, &e.Venue, &e.Remaining, &e.Description, &e.Category, &e.Tagline, &e.CoverURL, &e.Timezone, &e.FullPrice, &e.ChildPrice, &e.Capacity)
	e.Sold = e.Capacity - e.Remaining
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return e, err
}
func (r *BookingRepository) Event(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(r.db.QueryRowContext(ctx, "SELECT "+eventColumns+" FROM events WHERE id=? AND deleted_at IS NULL", id))
}
func (r *BookingRepository) Events(ctx context.Context) ([]domain.Event, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+eventColumns+" FROM events WHERE deleted_at IS NULL ORDER BY julianday(starts_at),id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (r *BookingRepository) SaveEvent(ctx context.Context, id string, v domain.EventInput, create bool) (domain.Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	remaining := v.Capacity
	if !create {
		current, err := scanEvent(tx.QueryRowContext(ctx, "SELECT "+eventColumns+" FROM events WHERE id=? AND deleted_at IS NULL", id))
		if err != nil {
			return domain.Event{}, err
		}
		if v.Capacity < current.Sold {
			return domain.Event{}, domain.ErrConflict
		}
		remaining = v.Capacity - current.Sold
	}
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO events (id,title,description,category,tagline,cover_url,starts_at,timezone,venue,full_price,child_price,capacity,remaining) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, v.Title, v.Description, v.Category, v.Tagline, v.CoverURL, v.Date, v.Timezone, v.Venue, v.FullPrice, v.ChildPrice, v.Capacity, remaining)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE events SET title=?,description=?,category=?,tagline=?,cover_url=?,starts_at=?,timezone=?,venue=?,full_price=?,child_price=?,capacity=?,remaining=? WHERE id=?`, v.Title, v.Description, v.Category, v.Tagline, v.CoverURL, v.Date, v.Timezone, v.Venue, v.FullPrice, v.ChildPrice, v.Capacity, remaining, id)
	}
	if err != nil {
		return domain.Event{}, err
	}
	result, err := scanEvent(tx.QueryRowContext(ctx, "SELECT "+eventColumns+" FROM events WHERE id=?", id))
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func (r *BookingRepository) DeleteEvent(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE events SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}
