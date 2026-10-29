package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"testing"
	"time"
)

func TestMailingConfirmationFreezesAudienceAndIsConcurrentSafe(t *testing.T) {
	db, _, _ := setup(t)
	ctx := context.Background()
	if _, err := sqlite.SeedDemoOrders(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewMailingRepository(db)
	m := usecase.NewMailings(repo)
	d, err := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Привет!"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("saving a draft published messages")
	}
	confirmation := domain.MailingConfirmation{ExpectedUpdatedAt: d.UpdatedAt, AudienceHash: d.AudienceHash}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := m.Confirm(ctx, d.ID, confirmation); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	run, err := m.Get(ctx, d.ID)
	if err != nil || run.Status != "running" || run.Progress.Total != 6 || len(run.Deliveries) != 6 {
		t.Fatalf("bad run: %+v %v", run, err)
	}
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&count)
	if count != 6 {
		t.Fatalf("duplicate commands: %d", count)
	}
	if _, err = m.Save(ctx, d.ID, domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Changed"}); !errors.Is(err, domain.ErrMailingLocked) {
		t.Fatal("started mailing editable", err)
	}
	if _, err = db.Exec("UPDATE events SET title='Новое название' WHERE id=?", sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE orders SET status='cancelled',cancelled_at=? WHERE checkout_id IN (SELECT id FROM checkouts WHERE event_id=?)", time.Now().Format(time.RFC3339Nano), sqlite.FirstEventID); err != nil {
		t.Fatal(err)
	}
	run, err = m.Get(ctx, d.ID)
	if err != nil || run.EventTitle != d.EventTitle || len(run.Deliveries) != 6 || run.Body != d.Body {
		t.Fatal("snapshot changed", err)
	}
	for _, delivery := range run.Deliveries {
		if eligible, err := repo.Eligible(ctx, delivery.ID); err != nil || eligible {
			t.Fatalf("cancelled recipient eligible: %v %v", eligible, err)
		}
	}
	again, err := m.Confirm(ctx, d.ID, domain.MailingConfirmation{})
	if err != nil || again.ConfirmedAt != run.ConfirmedAt {
		t.Fatal("repeat confirm not idempotent", err)
	}
}

func TestStalePreviewAndIncompleteMailingCannotLaunch(t *testing.T) {
	db, b, _ := setup(t)
	ctx := context.Background()
	m := usecase.NewMailings(sqlite.NewMailingRepository(db))
	d, _ := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Message"})
	preview := domain.MailingConfirmation{ExpectedUpdatedAt: d.UpdatedAt, AudienceHash: d.AudienceHash}
	if _, err := m.Confirm(ctx, d.ID, preview); err == nil {
		t.Fatal("empty audience accepted")
	}
	q := draft(t, b, false)
	if _, err := b.Confirm(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Confirm(ctx, d.ID, preview); !errors.Is(err, domain.ErrMailingChanged) {
		t.Fatal("stale audience accepted", err)
	}
	current, _ := m.Get(ctx, d.ID)
	preview = domain.MailingConfirmation{ExpectedUpdatedAt: current.UpdatedAt, AudienceHash: current.AudienceHash}
	if _, err := m.Save(ctx, d.ID, domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Changed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Confirm(ctx, d.ID, preview); !errors.Is(err, domain.ErrMailingChanged) {
		t.Fatal("stale text accepted", err)
	}
	empty, _ := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID})
	if _, err := m.Confirm(ctx, empty.ID, domain.MailingConfirmation{ExpectedUpdatedAt: empty.UpdatedAt, AudienceHash: empty.AudienceHash}); err == nil {
		t.Fatal("empty body accepted")
	}
	if _, err := m.Resume(ctx, d.ID); err == nil {
		t.Fatal("resume bypassed confirmation")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("invalid launch created commands")
	}
}

func TestResultsAreVersionedAndResumeOnlyRetriesFailed(t *testing.T) {
	db, _, _ := setup(t)
	ctx := context.Background()
	if _, err := sqlite.SeedDemoOrders(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewMailingRepository(db)
	m := usecase.NewMailings(repo)
	d, _ := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Message"})
	run, err := m.Confirm(ctx, d.ID, domain.MailingConfirmation{ExpectedUpdatedAt: d.UpdatedAt, AudienceHash: d.AudienceHash})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(id string, generation, version int, status string) {
		t.Helper()
		body, _ := json.Marshal(dispatch.Result{Schema: 1, ID: id, Generation: generation, Version: version, Status: status, Attempts: 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if err := repo.ApplyResult(ctx, body); err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range run.Deliveries {
		status := "sent"
		if i == 0 {
			status = "failed"
		}
		if i == 1 {
			status = "skipped"
		}
		apply(d.ID, 0, 2, status)
		apply(d.ID, 0, 2, status)
		apply(d.ID, 0, 1, "processing")
	}
	done, _ := m.Get(ctx, run.ID)
	if done.Status != "completed_with_errors" || done.Progress.Sent != 4 || done.Progress.Skipped != 1 || done.Progress.Failed != 1 {
		t.Fatalf("bad progress %+v", done.Progress)
	}
	resumed, err := m.Resume(ctx, run.ID)
	if err != nil || resumed.Status != "running" || resumed.Progress.Pending != 1 {
		t.Fatalf("bad resume %+v %v", resumed, err)
	}
	if _, err := m.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&count)
	if count != 7 {
		t.Fatalf("resume duplicated commands: %d", count)
	}
	apply(run.Deliveries[0].ID, 0, 99, "failed") // Stale generation must not regress a resumed delivery.
	current, _ := m.Get(ctx, run.ID)
	if current.Progress.Pending != 1 {
		t.Fatal("old generation applied")
	}
	apply(run.Deliveries[0].ID, 1, 3, "sent")
	done, _ = m.Get(ctx, run.ID)
	if done.Status != "completed" || done.Progress.Sent != 5 || done.Progress.Skipped != 1 {
		t.Fatal("not completed", done.Progress)
	}
	if _, err := m.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&count)
	if count != 7 {
		t.Fatal("resumed successful deliveries")
	}
	if !errors.Is(repo.ApplyResult(ctx, []byte("{}")), dispatch.ErrPoison) {
		t.Fatal("invalid result accepted")
	}
}

func TestEligibilityKeepsEmailWhileAnyPurchaseIsActive(t *testing.T) {
	db, b, _ := setup(t)
	ctx := context.Background()
	for range 2 {
		q := draft(t, b, false)
		if _, err := b.Confirm(ctx, q.ID); err != nil {
			t.Fatal(err)
		}
	}
	repo := sqlite.NewMailingRepository(db)
	m := usecase.NewMailings(repo)
	d, _ := m.Save(ctx, "", domain.MailingInput{EventID: sqlite.FirstEventID, Body: "Message"})
	run, err := m.Confirm(ctx, d.ID, domain.MailingConfirmation{ExpectedUpdatedAt: d.UpdatedAt, AudienceHash: d.AudienceHash})
	if err != nil || len(run.Deliveries) != 1 {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE orders SET status='cancelled',cancelled_at=? WHERE id=(SELECT id FROM orders LIMIT 1)", time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if eligible, err := repo.Eligible(ctx, run.Deliveries[0].ID); err != nil || !eligible {
		t.Fatal("second active order ignored", err)
	}
	if _, err := db.Exec("UPDATE orders SET status='cancelled',cancelled_at=?", time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if eligible, err := repo.Eligible(ctx, run.Deliveries[0].ID); err != nil || eligible {
		t.Fatal("all cancelled still eligible", err)
	}
}
