package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/openrouter"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"sync"
	"testing"
	"time"
)

func chatRequest(t *testing.T, r *sqlite.AssistantRepository) (string, string) {
	t.Helper()
	ctx := context.Background()
	thread, run := uuid.NewString(), uuid.NewString()
	if _, err := r.CreateThread(ctx, "owner", thread); err != nil {
		t.Fatal(err)
	}
	if err := r.Enqueue(ctx, "owner", thread, run, "Перенеси ивент и подготовь уведомление"); err != nil {
		t.Fatal(err)
	}
	return thread, run
}
func testProposal(t *testing.T, r *sqlite.AssistantRepository, events *sqlite.BookingRepository, thread, run string) domain.Proposal {
	t.Helper()
	e, err := events.Event(context.Background(), sqlite.FirstEventID)
	if err != nil {
		t.Fatal(err)
	}
	after := e.Input()
	after.Date = "2026-11-20T19:00:00+03:00"
	p, err := r.Propose(context.Background(), domain.Proposal{ID: run, RunID: run, ThreadID: thread, EventID: e.ID, Before: e.Input(), After: after, MailingBody: "Новое время — 20 ноября 2026, 19:00 МСК.", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func completeChat(t *testing.T, r *sqlite.AssistantRepository, thread, run string) {
	t.Helper()
	if err := r.Append(context.Background(), domain.ChatRun{ID: run, ThreadID: thread}, domain.AIMessage{Role: "assistant", Content: "Проверьте предложение."}, true); err != nil {
		t.Fatal(err)
	}
}
func TestChatApprovalAtomicIdempotentAndOwned(t *testing.T) {
	db, _, events := setup(t)
	ctx := context.Background()
	r := sqlite.NewAssistantRepository(db)
	thread, run := chatRequest(t, r)
	p := testProposal(t, r, events, thread, run)
	if _, err := r.Decide(ctx, "owner", thread, p.ID, true); !errors.Is(err, domain.ErrChatBusy) {
		t.Fatal("allowed decision during run", err)
	}
	completeChat(t, r, thread, run)
	if _, err := r.Details(ctx, "other", thread); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("other owner read chat", err)
	}
	if _, err := r.Decide(ctx, "other", thread, p.ID, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("other owner applied proposal", err)
	}
	// Simulate a DB write failure after event update: neither half may persist.
	if _, err := db.Exec(`CREATE TRIGGER fail_chat_draft BEFORE INSERT ON mailings BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Decide(ctx, "owner", thread, p.ID, true); err == nil {
		t.Fatal("expected failure")
	}
	e, _ := events.Event(ctx, p.EventID)
	if e.Input() != p.Before {
		t.Fatal("partial event update committed")
	}
	db.Exec("DROP TRIGGER fail_chat_draft")
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.Decide(ctx, "owner", thread, p.ID, true)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- result.MailingID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if got == "" || id != "" && id != got {
			t.Fatal("duplicate or missing draft")
		}
		id = got
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mailings").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate drafts", count)
	}
	db.QueryRow("SELECT COUNT(*) FROM message_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("agent started sending")
	}
	m, err := sqlite.NewMailingRepository(db).Mailing(ctx, id)
	if err != nil || m.Status != "draft" {
		t.Fatal("draft missing", err)
	}
	e, _ = events.Event(ctx, p.EventID)
	if e.Input() != p.After {
		t.Fatal("event was not updated")
	}
	if _, err = r.Decide(ctx, "owner", thread, p.ID, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("applied proposal was reversed", err)
	}
}
func TestChatStaleRejectAndMessageIdempotency(t *testing.T) {
	db, _, events := setup(t)
	ctx := context.Background()
	r := sqlite.NewAssistantRepository(db)
	thread, run := chatRequest(t, r)
	if err := r.Enqueue(ctx, "owner", thread, run, "Перенеси ивент и подготовь уведомление"); err != nil {
		t.Fatal(err)
	}
	if err := r.Enqueue(ctx, "owner", thread, run, "different"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("id reused", err)
	}
	if err := r.Enqueue(ctx, "owner", thread, uuid.NewString(), "second"); !errors.Is(err, domain.ErrChatBusy) {
		t.Fatal("parallel run", err)
	}
	p := testProposal(t, r, events, thread, run)
	completeChat(t, r, thread, run)
	changed := p.Before
	changed.Title = "Changed by another browser"
	if _, err := events.SaveEvent(ctx, p.EventID, changed, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Decide(ctx, "owner", thread, p.ID, true); !errors.Is(err, domain.ErrProposalStale) {
		t.Fatal("stale applied", err)
	}
	if _, err := r.Decide(ctx, "owner", thread, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Decide(ctx, "owner", thread, p.ID, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("rejected applied", err)
	}
	d, _ := r.Details(ctx, "owner", thread)
	if len(d.Runs) != 1 || d.Proposals[0].Status != "rejected" {
		t.Fatal("unexpected state", d)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mailings").Scan(&count)
	if count != 0 {
		t.Fatal("stale draft created")
	}
}
func toolCall(id, name string, args any) domain.ToolCall {
	b, _ := json.Marshal(args)
	var c domain.ToolCall
	c.ID = id
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = string(b)
	return c
}
func waitChat(t *testing.T, r *sqlite.AssistantRepository, thread string) domain.ChatDetails {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, err := r.Details(context.Background(), "owner", thread)
		if err != nil {
			t.Fatal(err)
		}
		if d.Runs[0].Status == "completed" || d.Runs[0].Status == "failed" {
			return d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("chat timeout")
	return domain.ChatDetails{}
}
func startAgent(t *testing.T, a *usecase.Assistant) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()
	return func() { cancel(); <-done }
}
func TestOpenRouterToolLoopAndRecovery(t *testing.T) {
	db, _, events := setup(t)
	ctx := context.Background()
	r := sqlite.NewAssistantRepository(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing API auth")
		}
		var v struct {
			Messages []domain.AIMessage `json:"messages"`
			Tools    []any              `json:"tools"`
		}
		if err := json.NewDecoder(req.Body).Decode(&v); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(v.Tools) != 5 {
			t.Error("missing tools")
		}
		last := v.Messages[len(v.Messages)-1]
		reply := domain.AIMessage{Role: "assistant"}
		switch last.Role {
		case "user":
			reply.ToolCalls = []domain.ToolCall{toolCall("read", "read_event", map[string]string{"event_id": sqlite.FirstEventID})}
		case "tool":
			if last.ToolCallID == "read" {
				var result struct {
					Version string `json:"version"`
				}
				json.Unmarshal([]byte(last.Content), &result)
				reply.ToolCalls = []domain.ToolCall{toolCall("prepare", "prepare_change", map[string]any{"event_id": sqlite.FirstEventID, "expected_version": result.Version, "patch": map[string]string{"date": "2026-11-20T19:00:00+03:00"}, "mailing_body": "Перенос на 20 ноября 2026, 19:00 МСК."})}
			} else {
				reply.Content = "Предложение готово, проверьте карточку."
			}
		default:
			t.Error("incomplete tool protocol")
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": reply, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	a := &usecase.Assistant{Repo: r, Events: usecase.NewEvents(events), Mailings: usecase.NewMailings(sqlite.NewMailingRepository(db)), Enabled: true, Model: &openrouter.Client{Key: "test-secret", Model: "fixture", Endpoint: server.URL, HTTP: server.Client(), MaxTokens: 4096}, Timeout: time.Second * 3, MaxSteps: 8, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	thread, _ := chatRequest(t, r)
	stop := startAgent(t, a)
	d := waitChat(t, r, thread)
	stop()
	if d.Runs[0].Status != "completed" || len(d.Proposals) != 1 {
		t.Fatalf("tool loop failed: %+v", d)
	}
	e, _ := events.Event(ctx, sqlite.FirstEventID)
	if e.Input() != d.Proposals[0].Before {
		t.Fatal("model bypassed approval")
	}
	// Crash window: proposal persisted, tool result not yet appended.
	thread, run := chatRequest(t, r)
	claimed, _, err := r.Next(ctx)
	if err != nil || claimed.ID != run {
		t.Fatal(err)
	}
	p := testProposal(t, r, events, thread, run)
	call := toolCall("prepare", "prepare_change", map[string]any{"event_id": p.EventID, "expected_version": domain.EventFingerprint(p.Before), "patch": map[string]string{"date": p.After.Date}, "mailing_body": p.MailingBody})
	if err = r.Append(ctx, claimed, domain.AIMessage{Role: "assistant", ToolCalls: []domain.ToolCall{call}}, false); err != nil {
		t.Fatal(err)
	}
	// A new repository and worker represent the next API process.
	a.Repo = sqlite.NewAssistantRepository(db)
	stop = startAgent(t, a)
	d = waitChat(t, r, thread)
	stop()
	if len(d.Proposals) != 1 || d.Runs[0].Status != "completed" {
		t.Fatal("restart lost or duplicated proposal", d)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM mailings").Scan(&count)
	if count != 0 {
		t.Fatal("recovery applied proposal without human")
	}
}

type chatModelFunc func(context.Context, []domain.AIMessage, []any) (domain.AIMessage, error)

func (f chatModelFunc) Complete(ctx context.Context, m []domain.AIMessage, tools []any) (domain.AIMessage, error) {
	return f(ctx, m, tools)
}
func TestChatFailureResumeAndForbiddenTool(t *testing.T) {
	db, _, events := setup(t)
	ctx := context.Background()
	repo := sqlite.NewAssistantRepository(db)
	thread, run := chatRequest(t, repo)
	// A hallucinated sending tool must be recorded as an error, never executed.
	if err := repo.Append(ctx, domain.ChatRun{ID: run, ThreadID: thread}, domain.AIMessage{Role: "assistant", ToolCalls: []domain.ToolCall{toolCall("bad", "confirm_mailing", map[string]string{"id": "any"})}}, false); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a := &usecase.Assistant{Repo: repo, Events: usecase.NewEvents(events), Mailings: usecase.NewMailings(sqlite.NewMailingRepository(db)), Enabled: true, Timeout: time.Second, MaxSteps: 8, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Model: chatModelFunc(func(_ context.Context, m []domain.AIMessage, _ []any) (domain.AIMessage, error) {
		calls++
		last := m[len(m)-1]
		if last.Role != "tool" || !strings.Contains(last.Content, "error") {
			t.Error("unknown tool not rejected")
		}
		if calls == 1 {
			return domain.AIMessage{}, errors.New("OpenRouter HTTP 503")
		}
		return domain.AIMessage{Role: "assistant", Content: "Отправка доступна только в интерфейсе рассылки."}, nil
	})}
	stop := startAgent(t, a)
	d := waitChat(t, repo, thread)
	stop()
	if d.Runs[0].Status != "failed" {
		t.Fatal("provider failure lost")
	}
	if err := repo.Retry(ctx, "other", thread, run); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign retry permitted")
	}
	if err := repo.Retry(ctx, "owner", thread, run); err != nil {
		t.Fatal(err)
	}
	stop = startAgent(t, a)
	d = waitChat(t, repo, thread)
	stop()
	if d.Runs[0].Status != "completed" || len(d.Messages) != 4 {
		t.Fatal("resume duplicated a step", d)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM message_outbox").Scan(&count)
	if count != 0 {
		t.Fatal("unknown tool sent messages")
	}
}
