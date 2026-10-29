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
	authinfra "sqoch-ticket/backend/internal/infrastructure/auth"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
	"time"
)

func TestAdminAuthentication(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions, err := sqlite.NewAdminSessions(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := authinfra.NewPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	tokens := authinfra.NewTokens(strings.Repeat("s", 32))
	service := usecase.NewAuth("organizer@example.com", passwords, tokens, sessions, time.Hour)
	const origin = "https://admin.example.com"
	handler := delivery.NewAdminHandler(service, origin, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(method, path, body, from string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", from)
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assertStatus := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("want %d, got %d: %s", want, w.Code, w.Body.String())
		}
	}
	assertStatus(request("GET", "/api/admin/me", "", "", nil), 401)
	credentials := `{"email":"organizer@example.com","password":"test-password-123"}`
	for _, from := range []string{"", "https://evil.example", "null"} {
		assertStatus(request("POST", "/api/admin/login", credentials, from, nil), 403)
	}
	for _, body := range []string{`{"email":"organizer@example.com","password":"wrong"}`, `{"email":"unknown@example.com","password":"test-password-123"}`} {
		w := request("POST", "/api/admin/login", body, origin, nil)
		assertStatus(w, 401)
		if !strings.Contains(w.Body.String(), "invalid_credentials") {
			t.Fatal("credentials must share one error")
		}
	}
	assertStatus(request("POST", "/api/admin/login", credentials+` {}`, origin, nil), 400)
	assertStatus(request("POST", "/api/admin/login", `{"email":"x","password":"y","role":"organizer"}`, origin, nil), 400)
	w := request("POST", "/api/admin/login", credentials, origin, nil)
	assertStatus(w, 200)
	var result usecase.AdminSession
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Email != "organizer@example.com" || result.ExpiresAt.IsZero() || strings.Contains(w.Body.String(), "token") {
		t.Fatal("unexpected login payload")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("expected session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" || cookie.Path != "/api/admin" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	assertStatus(request("GET", "/api/admin/me", "", "", cookie), 200)
	// A new handler/store uses the same DB: restart does not erase sessions.
	store2, err := sqlite.NewAdminSessions(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	restarted := usecase.NewAuth("organizer@example.com", passwords, tokens, store2, time.Hour)
	if _, err := restarted.Authenticate(ctx, cookie.Value); err != nil {
		t.Fatal(err)
	}
	tampered := *cookie
	parts := strings.Split(tampered.Value, ".")
	parts[1] = "eyJzdWIiOiJhdHRhY2tlciJ9"
	tampered.Value = strings.Join(parts, ".")
	assertStatus(request("GET", "/api/admin/me", "", "", &tampered), 401)
	assertStatus(request("POST", "/api/admin/logout", "{}", "https://evil.example", cookie), 403)
	assertStatus(request("GET", "/api/admin/me", "", "", cookie), 200)
	logout := request("POST", "/api/admin/logout", "{}", origin, cookie)
	assertStatus(logout, 204)
	if logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout must clear cookie")
	}
	assertStatus(request("GET", "/api/admin/me", "", "", cookie), 401)
	assertStatus(request("POST", "/api/admin/logout", "{}", origin, cookie), 204)
	// A correctly signed token without a persisted session is rejected.
	unknown, err := tokens.Sign(usecase.AdminSession{ID: "unknown", Email: "organizer@example.com", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(request("GET", "/api/admin/me", "", "", &http.Cookie{Name: cookie.Name, Value: unknown}), 401)
	assertStatus(request("GET", "/api/admin/future", "", "", nil), 401)
}
