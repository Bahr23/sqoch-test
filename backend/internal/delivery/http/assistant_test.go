package http_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	delivery "sqoch-ticket/backend/internal/delivery/http"
	"sqoch-ticket/backend/internal/domain"
	authinfra "sqoch-ticket/backend/internal/infrastructure/auth"
	"sqoch-ticket/backend/internal/infrastructure/openrouter"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
	"time"
)

func chatHTTP(t *testing.T, origin string) (http.Handler, *usecase.Assistant, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	sessions, err := sqlite.NewAdminSessions(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	password, err := authinfra.NewPassword("browser-test-password")
	if err != nil {
		t.Fatal(err)
	}
	auth := usecase.NewAuth("test@sqoch.local", password, authinfra.NewTokens(strings.Repeat("x", 32)), sessions, time.Hour)
	token, _, err := auth.Login(ctx, "test@sqoch.local", "browser-test-password")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &usecase.Assistant{Repo: sqlite.NewAssistantRepository(db), Events: usecase.NewEvents(sqlite.NewBookingRepository(db)), Mailings: usecase.NewMailings(sqlite.NewMailingRepository(db)), Timeout: 30 * time.Second, MaxSteps: 8, Logger: logger}
	protected := http.NewServeMux()
	protected.Handle("/api/admin/assistant/", delivery.NewAssistantHandler(a, logger))
	protected.Handle("/api/admin/", delivery.NewOrganizerRoutes(a.Events, a.Mailings, logger))
	return delivery.NewAdminHandler(auth, origin, logger, protected), a, &http.Cookie{Name: "sqoch_admin", Value: token}
}
func TestChatHTTPAuthenticationAndDisabledModel(t *testing.T) {
	const origin = "https://admin.example.com"
	handler, a, cookie := chatHTTP(t, origin)
	req := func(method, path, body, from string, c *http.Cookie) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", from)
		r.Header.Set("Content-Type", "application/json")
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if status := req("GET", "/api/admin/assistant/threads", "", "", nil); status != 401 {
		t.Fatal(status)
	}
	id := uuid.NewString()
	body := `{"id":"` + id + `"}`
	if status := req("POST", "/api/admin/assistant/threads", body, "https://evil.example", cookie); status != 403 {
		t.Fatal(status)
	}
	if status := req("POST", "/api/admin/assistant/threads", body, origin, cookie); status != 201 {
		t.Fatal(status)
	}
	if status := req("POST", "/api/admin/assistant/threads/"+id+"/messages", `{"id":"`+uuid.NewString()+`","message":"Привет"}`, origin, cookie); status != 503 {
		t.Fatal(status)
	}
	other, err := a.Repo.CreateThread(context.Background(), "someone-else", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if status := req("GET", "/api/admin/assistant/threads/"+other.ID, "", "", cookie); status != 404 {
		t.Fatal(status)
	}
}

// An opt-in browser harness: actual production frontend, API and tool loop with
// an isolated temporary DB and a local OpenRouter fixture. Never uses .env keys.
func TestChatBrowserFixture(t *testing.T) {
	if os.Getenv("SQOCH_CHAT_BROWSER_TEST") != "1" {
		t.Skip("opt-in browser fixture")
	}
	const origin = "http://localhost:18080"
	handler, a, _ := chatHTTP(t, origin)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []domain.AIMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		last := input.Messages[len(input.Messages)-1]
		reply := domain.AIMessage{Role: "assistant"}
		call := func(id, name string, args any) domain.ToolCall {
			b, _ := json.Marshal(args)
			var c domain.ToolCall
			c.ID = id
			c.Type = "function"
			c.Function.Name = name
			c.Function.Arguments = string(b)
			return c
		}
		if last.Role == "user" {
			reply.ToolCalls = []domain.ToolCall{call("read", "read_event", map[string]string{"event_id": sqlite.FirstEventID})}
		} else if last.ToolCallID == "read" {
			var v struct {
				Version string `json:"version"`
			}
			json.Unmarshal([]byte(last.Content), &v)
			reply.ToolCalls = []domain.ToolCall{call("prepare", "prepare_change", map[string]any{"event_id": sqlite.FirstEventID, "expected_version": v.Version, "patch": map[string]string{"date": "2026-11-20T19:00:00+03:00"}, "mailing_body": "Друзья, встреча «За пределами привычного» переносится на 20 ноября 2026 года, 19:00 (МСК).\n\nМесто остаётся прежним: Москва · Пространство «Орбита». Ваши билеты действительны. До встречи!"})}
		} else {
			reply.Content = "Подготовил перенос на 20 ноября 2026, 19:00 (Europe/Moscow), и текст уведомления. Проверьте карточку: после подтверждения сохранится ивент и появится черновик. Отправка запускается отдельно."
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": reply, "finish_reason": "stop"}}})
	}))
	defer fixture.Close()
	a.Enabled = true
	a.ModelName = "Локальная тестовая модель"
	a.Model = &openrouter.Client{Key: "fixture", Model: "fixture", Endpoint: fixture.URL, HTTP: fixture.Client(), MaxTokens: 4096}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()
	defer func() { cancel(); <-done }()
	root, err := filepath.Abs("../../../../frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/admin/", handler)
	mux.Handle("/assets/", http.FileServer(http.Dir(root)))
	mux.Handle("/fonts/", http.FileServer(http.Dir(root)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(root, r.URL.Path)); err == nil && r.URL.Path != "/" {
			http.FileServer(http.Dir(root)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "admin.html"))
	})
	server := http.Server{Addr: "127.0.0.1:18080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); server.Close() }()
	t.Log("Browser fixture: http://localhost:18080/chat · test@sqoch.local / browser-test-password")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
