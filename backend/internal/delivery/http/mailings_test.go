package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	delivery "sqoch-ticket/backend/internal/delivery/http"
	"sqoch-ticket/backend/internal/domain"
	authinfra "sqoch-ticket/backend/internal/infrastructure/auth"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
	"time"
)

func TestMailingAPIRequiresAuthenticatedExplicitConfirmation(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "mailings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = sqlite.SeedDemoOrders(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	sessions, err := sqlite.NewAdminSessions(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := authinfra.NewPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	auth := usecase.NewAuth("a@b.ru", passwords, authinfra.NewTokens(strings.Repeat("s", 32)), sessions, time.Hour)
	token, _, err := auth.Login(ctx, "a@b.ru", "test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	routes := delivery.NewOrganizerRoutes(usecase.NewEvents(sqlite.NewBookingRepository(db)), usecase.NewMailings(sqlite.NewMailingRepository(db)), logger)
	handler := delivery.NewAdminHandler(auth, "http://admin.localhost:8080", logger, routes)
	call := func(method, path, body string, authenticated bool, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "sqoch_admin", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	origin := "http://admin.localhost:8080"
	base := "/api/admin/mailings"
	audiencePath := "/api/admin/events/" + sqlite.FirstEventID + "/recipients"
	for _, r := range []struct{ method, path string }{{"GET", base}, {"POST", base}, {"GET", base + "/" + sqlite.FirstEventID}, {"PUT", base + "/" + sqlite.FirstEventID}, {"GET", audiencePath}} {
		if w := call(r.method, r.path, `{}`, false, origin); w.Code != 401 {
			t.Fatalf("anonymous %s: %d", r.path, w.Code)
		}
	}
	input := `{"event_id":"` + sqlite.FirstEventID + `","body":"Привет, гости!"}`
	if w := call("POST", base, input, true, "https://other.example.com"); w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	w := call("GET", audiencePath, "", true, origin)
	var people []domain.MailingRecipient
	if err := json.Unmarshal(w.Body.Bytes(), &people); err != nil || w.Code != 200 || len(people) != 6 {
		t.Fatalf("audience: %s %v", w.Body.String(), err)
	}
	w = call("POST", base, input, true, origin)
	var draft domain.MailingDetails
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil || w.Code != 201 || draft.Status != "draft" || draft.ID == "" {
		t.Fatalf("create: %d %s %v", w.Code, w.Body.String(), err)
	}
	path := base + "/" + draft.ID
	if w.Header().Get("Location") != path || len(draft.Recipients) != 6 {
		t.Fatal("missing detail")
	}
	for _, body := range []string{`null`, `{} {}`, `{"event_id":null,"body":"x"}`, `{"event_id":"` + sqlite.FirstEventID + `","body":null}`, `{"event_id":"` + sqlite.FirstEventID + `","body":"x","status":"sent"}`, `{"event_id":"` + sqlite.FirstEventID + `","body":"x","recipients":[]}`} {
		if w := call("POST", base, body, true, origin); w.Code != 400 {
			t.Fatalf("invalid payload %s: %d", body, w.Code)
		}
	}
	for _, body := range []string{`{"event_id":"","body":"x"}`, `{"event_id":"00000000-0000-0000-0000-000000000000","body":"x"}`, `{"event_id":"` + sqlite.FirstEventID + `","body":"` + strings.Repeat("я", 10001) + `"}`} {
		if w := call("POST", base, body, true, origin); w.Code != 422 {
			t.Fatalf("invalid content: %d %s", w.Code, w.Body.String())
		}
	}
	if w := call("PUT", path, `{"event_id":"`+sqlite.SecondEventID+`","body":"Обновлено"}`, true, origin); w.Code != 200 {
		t.Fatal("update", w.Body.String())
	}
	w = call("GET", path, "", true, origin)
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil || draft.EventID != sqlite.SecondEventID || draft.Body != "Обновлено" {
		t.Fatal("read after update", w.Body.String())
	}
	if w := call("POST", path+"/send", `{}`, true, origin); w.Code != 404 {
		t.Fatal("sending route unexpectedly enabled")
	}
	if w := call("PUT", base+"/00000000-0000-0000-0000-000000000000", input, true, origin); w.Code != 404 {
		t.Fatal("missing draft accepted")
	}
	w = call("GET", base, "", true, origin)
	var list []domain.Mailing
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].Status != "draft" {
		t.Fatal("list", w.Body.String())
	}
	for _, action := range []string{"/confirm", "/resume"} {
		if w := call("POST", path+action, "{}", false, origin); w.Code != 401 {
			t.Fatal("anonymous action allowed")
		}
		if w := call("POST", path+action, "{}", true, "https://evil.example"); w.Code != 403 {
			t.Fatal("cross-origin action allowed")
		}
		for _, input := range []string{"null", "[]", "{} {}", "{\"unexpected\":true}"} {
			if w := call("POST", path+action, input, true, origin); w.Code != 400 {
				t.Fatalf("invalid action input %s: %d", input, w.Code)
			}
		}
	}
	if w := call("POST", path+"/resume", "{}", true, origin); w.Code != 422 {
		t.Fatal("resume bypassed confirmation", w.Code)
	}
	if w := call("POST", path+"/confirm", "{}", true, origin); w.Code != 409 {
		t.Fatal("stale confirmation accepted", w.Code)
	}
	var commands int
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&commands)
	if commands != 0 {
		t.Fatal("commands created before explicit confirmation")
	}
	payload, _ := json.Marshal(domain.MailingConfirmation{ExpectedUpdatedAt: draft.UpdatedAt, AudienceHash: draft.AudienceHash})
	for range 2 {
		w := call("POST", path+"/confirm", string(payload), true, origin)
		var run domain.MailingDetails
		if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil || w.Code != 200 || run.Status != "running" || run.Progress.Total != 6 {
			t.Fatal("confirmation", w.Code, w.Body.String())
		}
	}
	db.QueryRow("SELECT count(*) FROM message_outbox").Scan(&commands)
	if commands != 6 {
		t.Fatal("duplicate confirmation", commands)
	}
	if w := call("PUT", path, input, true, origin); w.Code != 409 {
		t.Fatal("editing started mailing allowed", w.Code)
	}
}

type eligibilityFake func(context.Context, string) (bool, error)

func (f eligibilityFake) Eligible(ctx context.Context, id string) (bool, error) { return f(ctx, id) }
func TestInternalEligibilityRequiresServiceToken(t *testing.T) {
	calls := 0
	checker := eligibilityFake(func(context.Context, string) (bool, error) { calls++; return true, nil })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := delivery.NewEligibilityHandler(checker, strings.Repeat("s", 32), logger)
	for _, token := range []string{"", "Bearer wrong", "Bearer " + strings.Repeat("s", 32)} {
		r := httptest.NewRequest("GET", "/internal/notifications/test/eligibility", nil)
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if token == "Bearer "+strings.Repeat("s", 32) {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
	}
	if calls != 1 {
		t.Fatal("unauthorized eligibility lookup")
	}
}
