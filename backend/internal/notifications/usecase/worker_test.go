package usecase_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sqoch-ticket/backend/internal/dispatch"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	infra "sqoch-ticket/backend/internal/notifications/infrastructure"
	"sqoch-ticket/backend/internal/notifications/usecase"
	"testing"
	"time"

	"github.com/google/uuid"
)

type eligibility struct {
	active bool
	err    error
	calls  int
}

func (e *eligibility) Eligible(context.Context, string) (bool, error) {
	e.calls++
	return e.active, e.err
}

func database(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func command() dispatch.Command {
	return dispatch.Command{Schema: 1, ID: uuid.NewString(), MailingID: uuid.NewString(), EventID: uuid.NewString(), Email: "guest@example.com", Subject: "Ивент", Body: "Привет!"}
}
func ingest(t *testing.T, store *infra.Store, c dispatch.Command) {
	t.Helper()
	payload, _ := json.Marshal(c)
	if err := store.Ingest(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
}
func status(t *testing.T, store *infra.Store, id, expected string) {
	t.Helper()
	var value string
	if err := store.DB.QueryRow("SELECT status FROM notification_jobs WHERE id=?", id).Scan(&value); err != nil || value != expected {
		t.Fatalf("status %s want %s: %v", value, expected, err)
	}
}
func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func fixture(t *testing.T, mode string) (*infra.Store, *infra.MockSender, *eligibility, *usecase.Worker) {
	t.Helper()
	dir := t.TempDir()
	s := &infra.Store{DB: database(t, filepath.Join(dir, "jobs.db")), ResultQueue: "results"}
	m := &infra.MockSender{DB: database(t, filepath.Join(dir, "provider.db")), Mode: mode}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	e := &eligibility{active: true}
	w := &usecase.Worker{Repository: s, Sender: m, Eligibility: e, Lease: time.Second, Timeout: time.Second, RetryBase: time.Second, MaxAttempts: 3}
	return s, m, e, w
}
func step(t *testing.T, w *usecase.Worker, now time.Time) {
	t.Helper()
	if did, err := w.Step(context.Background(), now); err != nil || !did {
		t.Fatalf("step: processed=%v err=%v", did, err)
	}
}

func TestMockFailuresRetryWithoutDuplicateAcceptance(t *testing.T) {
	for _, mode := range []string{"none", "temporary_once", "after_accept_once"} {
		t.Run(mode, func(t *testing.T) {
			s, m, e, w := fixture(t, mode)
			c := command()
			ingest(t, s, c)
			ingest(t, s, c)
			now := time.Now()
			step(t, w, now)
			if mode != "none" {
				status(t, s, c.ID, "retry_wait")
				if did, err := w.Step(context.Background(), now.Add(500*time.Millisecond)); did || err != nil {
					t.Fatal("retry delay ignored", err)
				}
				if mode == "after_accept_once" {
					e.active = false
				} // Acceptance already happened, so later cancellation cannot erase it.
				step(t, w, now.Add(2*time.Second))
			}
			status(t, s, c.ID, "sent")
			ingest(t, s, c)
			if did, err := w.Step(context.Background(), now.Add(time.Hour)); did || err != nil {
				t.Fatal("successful job reclaimed", err)
			}
			if n := count(t, m.DB, "SELECT count(*) FROM mock_acceptances"); n != 1 {
				t.Fatal("duplicate acceptance", n)
			}
			expectedCalls := 1
			if mode == "temporary_once" {
				expectedCalls = 2
			}
			if n := count(t, m.DB, "SELECT calls FROM mock_calls"); n != expectedCalls {
				t.Fatal("unexpected provider calls", n)
			}
		})
	}
}

func TestEligibilityFailuresNeverSendAndAreRechecked(t *testing.T) {
	s, m, e, w := fixture(t, "none")
	c := command()
	ingest(t, s, c)
	e.err = errors.New("backend down")
	now := time.Now()
	step(t, w, now)
	status(t, s, c.ID, "retry_wait")
	if count(t, m.DB, "SELECT count(*) FROM mock_calls") != 0 {
		t.Fatal("sent without eligibility")
	}
	e.err = nil
	e.active = false
	step(t, w, now.Add(2*time.Second))
	status(t, s, c.ID, "skipped")
	if count(t, m.DB, "SELECT count(*) FROM mock_acceptances") != 0 || e.calls != 2 {
		t.Fatal("cancelled order sent or not rechecked")
	}
}

func TestResumeOnlyRetriesFailedAndUsesSameProviderKey(t *testing.T) {
	s, m, _, w := fixture(t, "always_fail")
	c := command()
	ingest(t, s, c)
	now := time.Now()
	for _, seconds := range []int{0, 2, 5} {
		step(t, w, now.Add(time.Duration(seconds)*time.Second))
	}
	status(t, s, c.ID, "failed")
	if count(t, m.DB, "SELECT count(*) FROM mock_acceptances") != 0 {
		t.Fatal("failed job accepted")
	}
	m.Mode = "none"
	c.Generation++
	ingest(t, s, c)
	ingest(t, s, c)
	step(t, w, now.Add(10*time.Second))
	status(t, s, c.ID, "sent")
	if count(t, m.DB, "SELECT count(*) FROM mock_acceptances") != 1 {
		t.Fatal("resume duplicated acceptance")
	}
	// Even an unexpected newer generation cannot cause another accepted send.
	c.Generation++
	ingest(t, s, c)
	if did, err := w.Step(context.Background(), now.Add(time.Hour)); did || err != nil {
		t.Fatal("terminal delivery resent", err)
	}
	if count(t, m.DB, "SELECT calls FROM mock_calls") != 4 {
		t.Fatal("unexpected provider calls")
	}
}

func TestCrashAfterProviderAcceptanceRecoversAfterReopeningDatabases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	jobsPath, providerPath := filepath.Join(dir, "jobs.db"), filepath.Join(dir, "provider.db")
	s := &infra.Store{DB: database(t, jobsPath), ResultQueue: "results"}
	m := &infra.MockSender{DB: database(t, providerPath), Mode: "none"}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := command()
	ingest(t, s, c)
	now := time.Now()
	old, err := s.Claim(ctx, now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Send(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Simulate process death before Finish: only provider acceptance and lease were committed.
	s.DB.Close()
	m.DB.Close()
	s = &infra.Store{DB: database(t, jobsPath), ResultQueue: "results"}
	m = &infra.MockSender{DB: database(t, providerPath), Mode: "none"}
	e := &eligibility{active: false}
	w := &usecase.Worker{Repository: s, Sender: m, Eligibility: e, Lease: time.Second, Timeout: time.Second, RetryBase: time.Second, MaxAttempts: 3}
	step(t, w, now.Add(2*time.Second))
	status(t, s, c.ID, "sent")
	if err := s.Finish(ctx, old, "failed", "stale worker", now); err != nil {
		t.Fatal(err)
	}
	status(t, s, c.ID, "sent")
	if e.calls != 0 || count(t, m.DB, "SELECT calls FROM mock_calls") != 1 {
		t.Fatal("accepted send was repeated")
	}
	if count(t, s.DB, "SELECT count(*) FROM message_outbox WHERE published_at IS NULL") != 3 {
		t.Fatal("durable results missing")
	}
}

func TestLeaseAndMessageIdentityProtectJobs(t *testing.T) {
	s, _, _, _ := fixture(t, "none")
	c := command()
	ingest(t, s, c)
	now := time.Now()
	old, err := s.Claim(context.Background(), now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(context.Background(), now.Add(500*time.Millisecond), time.Second); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("active lease claimed", err)
	}
	current, err := s.Claim(context.Background(), now.Add(2*time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(context.Background(), old, "sent", "", now); err != nil {
		t.Fatal(err)
	}
	status(t, s, c.ID, "processing")
	if err := s.Finish(context.Background(), current, "sent", "", now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*dispatch.Command){func(c *dispatch.Command) { c.Body = "different" }, func(c *dispatch.Command) { c.ID = uuid.NewString() }} {
		invalid := c
		change(&invalid)
		payload, _ := json.Marshal(invalid)
		if err := s.Ingest(context.Background(), payload); !errors.Is(err, dispatch.ErrPoison) {
			t.Fatal("conflicting message accepted", err)
		}
	}
	status(t, s, c.ID, "sent")
}

func TestSameEmailCanReceiveDifferentMailings(t *testing.T) {
	s, m, _, w := fixture(t, "none")
	first, second := command(), command()
	second.EventID = first.EventID
	for _, c := range []dispatch.Command{first, second} {
		ingest(t, s, c)
		ingest(t, s, c)
	}
	now := time.Now()
	step(t, w, now)
	step(t, w, now)
	status(t, s, first.ID, "sent")
	status(t, s, second.ID, "sent")
	if n := count(t, m.DB, "SELECT count(*) FROM mock_acceptances"); n != 2 {
		t.Fatalf("deduplication must be per mailing, not per email: %d", n)
	}
	if did, err := w.Step(context.Background(), now.Add(time.Hour)); did || err != nil {
		t.Fatal("duplicate job remained", err)
	}
}

type unavailableLookup struct {
	*infra.MockSender
	err error
}

func (s *unavailableLookup) Lookup(ctx context.Context, c dispatch.Command) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.MockSender.Lookup(ctx, c)
}

func TestLookupFailureDoesNotResendAnAlreadyAcceptedNotification(t *testing.T) {
	s, m, e, w := fixture(t, "none")
	c := command()
	ingest(t, s, c)
	if err := m.Send(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	sender := &unavailableLookup{MockSender: m, err: errors.New("provider lookup unavailable")}
	w.Sender = sender
	now := time.Now()
	step(t, w, now)
	status(t, s, c.ID, "retry_wait")
	if e.calls != 0 || count(t, m.DB, "SELECT calls FROM mock_calls") != 1 {
		t.Fatal("uncertain acceptance triggered a new send")
	}
	sender.err = nil
	step(t, w, now.Add(2*time.Second))
	status(t, s, c.ID, "sent")
	if e.calls != 0 || count(t, m.DB, "SELECT calls FROM mock_calls") != 1 {
		t.Fatal("accepted notification resent after lookup recovered")
	}
}

func TestSenderTimeoutCanRecoverWithoutAcceptance(t *testing.T) {
	s, m, _, w := fixture(t, "none")
	c := command()
	ingest(t, s, c)
	m.Delay = time.Hour
	w.Timeout = 10 * time.Millisecond
	now := time.Now()
	step(t, w, now)
	status(t, s, c.ID, "retry_wait")
	if count(t, m.DB, "SELECT count(*) FROM mock_acceptances") != 0 {
		t.Fatal("timed-out sender accepted a message")
	}
	m.Delay = 0
	w.Timeout = time.Second
	step(t, w, now.Add(2*time.Second))
	status(t, s, c.ID, "sent")
	if count(t, m.DB, "SELECT count(*) FROM mock_acceptances") != 1 {
		t.Fatal("notification did not recover after sender timeout")
	}
}
