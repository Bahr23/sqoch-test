package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sqoch-ticket/backend/internal/domain"
	"time"
)

type DemoOrdersReport struct {
	Created  int               `json:"created"`
	Existing int               `json:"existing"`
	Events   []DemoEventOrders `json:"events"`
}

type DemoEventOrders struct {
	EventID       string `json:"event_id"`
	Title         string `json:"title"`
	Active        int    `json:"active"`
	Cancelled     int    `json:"cancelled"`
	ActiveTickets int    `json:"active_tickets"`
}

type demoBuyer struct {
	email     string
	people    []domain.ParticipantInput
	cancelled bool
}

func demoPerson(name string, age int) domain.ParticipantInput {
	return domain.ParticipantInput{Name: name, Age: &age}
}

func demoBuyers() []demoBuyer {
	return []demoBuyer{
		{"anna.demo@example.com", []domain.ParticipantInput{demoPerson("Анна Смирнова", 28)}, false},
		{"alex.demo@example.com", []domain.ParticipantInput{demoPerson("Алексей Волков", 34), demoPerson("Мария Волкова", 32)}, false},
		{"anna.demo@example.com", []domain.ParticipantInput{demoPerson("Анна Смирнова", 28), demoPerson("София Смирнова", 13)}, true},
		{"olga.demo@example.com", []domain.ParticipantInput{demoPerson("Ольга Кузнецова", 39), demoPerson("Илья Кузнецов", 17)}, false},
		{"family.demo@example.com", []domain.ParticipantInput{demoPerson("Дмитрий Соколов", 36), demoPerson("Елена Соколова", 35), demoPerson("Артём Соколов", 8), demoPerson("Алиса Соколова", 4)}, false},
		{"alex.demo@example.com", []domain.ParticipantInput{demoPerson("Алексей Волков", 34)}, true},
		{"irina.demo@example.com", []domain.ParticipantInput{demoPerson("Ирина Морозова", 45), demoPerson("Михаил Морозов", 12)}, false},
		{"sergey.demo@example.com", []domain.ParticipantInput{demoPerson("Сергей Петров", 70), demoPerson("Наталья Петрова", 68)}, false},
		{"family.demo@example.com", []domain.ParticipantInput{demoPerson("Дмитрий Соколов", 36), demoPerson("Артём Соколов", 8), demoPerson("Виктор Соколов", 14)}, true},
		{"anna.demo@example.com", []domain.ParticipantInput{demoPerson("Павел Смирнов", 19)}, false},
	}
}

// SeedDemoOrders adds one named fixture batch only on explicit CLI invocation.
// Stable IDs prevent duplicate orders and stock deductions on repeated runs.
// Everything is committed together; existing purchases are never rewritten.
func SeedDemoOrders(ctx context.Context, db *sql.DB, now time.Time) (DemoOrdersReport, error) {
	var report DemoOrdersReport
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	for eventIndex, eventID := range []string{FirstEventID, SecondEventID} {
		event, err := scanEvent(tx.QueryRowContext(ctx, "SELECT "+eventColumns+" FROM events WHERE id=? AND deleted_at IS NULL", eventID))
		if err != nil {
			return DemoOrdersReport{}, fmt.Errorf("demo event %s: %w", eventID, err)
		}
		stats := DemoEventOrders{EventID: eventID, Title: event.Title}
		for i, buyer := range demoBuyers() {
			id := fmt.Sprintf("demo-orders-v1-%d-%02d", eventIndex+1, i+1)
			checkoutID := id + "-checkout"
			var exists bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM orders WHERE id=?)", id).Scan(&exists); err != nil {
				return DemoOrdersReport{}, err
			}
			if exists {
				report.Existing++
				continue
			}
			participants, total, err := domain.PriceOrder(domain.OrderInput{Email: buyer.email, Participants: buyer.people}, event)
			if err != nil {
				return DemoOrdersReport{}, err
			}
			people, err := json.Marshal(participants)
			if err != nil {
				return DemoOrdersReport{}, err
			}
			snapshot, err := json.Marshal(event)
			if err != nil {
				return DemoOrdersReport{}, err
			}
			created := now.UTC().Add(-time.Duration(20-i*2-eventIndex) * 24 * time.Hour)
			status := domain.OrderActive
			var cancelledAt any
			if buyer.cancelled {
				status = domain.OrderCancelled
				cancelledAt = created.Add(2 * time.Hour).Format(time.RFC3339)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO checkouts (id,event_id,email,participants,total,expires_at,event_snapshot) VALUES (?,?,?,?,?,?,?)`, checkoutID, eventID, buyer.email, string(people), total, created.Add(15*time.Minute).Format(time.RFC3339), string(snapshot)); err != nil {
				return DemoOrdersReport{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO orders (id,checkout_id,number,created_at,status,cancelled_at) VALUES (?,?,?,?,?,?)`, id, checkoutID, fmt.Sprintf("SQ-DEMO-%d-%02d", eventIndex+1, i+1), created.Format(time.RFC3339), status, cancelledAt); err != nil {
				return DemoOrdersReport{}, err
			}
			// Historical cancelled purchases hold no seats. Only active tickets
			// affect the current inventory, using the same stock guard as Confirm.
			if !buyer.cancelled {
				result, err := tx.ExecContext(ctx, "UPDATE events SET remaining=remaining-? WHERE id=? AND remaining>=?", len(participants), eventID, len(participants))
				if err != nil {
					return DemoOrdersReport{}, err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return DemoOrdersReport{}, err
				}
				if n != 1 {
					return DemoOrdersReport{}, fmt.Errorf("demo event %s: %w", eventID, domain.ErrSoldOut)
				}
				event.Remaining -= len(participants)
				event.Sold += len(participants)
			}
			report.Created++
		}
		if err := tx.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(o.status='active'),0), COALESCE(SUM(o.status='cancelled'),0),
 COALESCE(SUM(CASE WHEN o.status='active' THEN json_array_length(c.participants) ELSE 0 END),0)
 FROM orders o JOIN checkouts c ON c.id=o.checkout_id
 WHERE c.event_id=? AND o.id LIKE 'demo-orders-v1-%'`, eventID).Scan(&stats.Active, &stats.Cancelled, &stats.ActiveTickets); err != nil {
			return DemoOrdersReport{}, err
		}
		report.Events = append(report.Events, stats)
	}
	if err := tx.Commit(); err != nil {
		return DemoOrdersReport{}, err
	}
	return report, nil
}
