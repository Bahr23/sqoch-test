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

func TestEventRESTRequiresAuthAndSupportsCRUD(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "rest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = sqlite.Migrate(ctx, db); err != nil {
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
	events := usecase.NewEvents(sqlite.NewBookingRepository(db))
	handler := delivery.NewAdminHandler(auth, "http://admin.localhost:8080", logger, delivery.NewEventManagementHandler(events, logger))
	call := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", "http://admin.localhost:8080")
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "sqoch_admin", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		w := call(method, "/api/admin/events", `{}`, false)
		if w.Code != 401 {
			t.Fatalf("%s anonymous: %d", method, w.Code)
		}
	}
	body := `{"title":"Новый ивент","description":"Описание","category":"Музыка","tagline":"Вечер","cover_url":"","date":"2027-01-01T18:00:00+03:00","timezone":"Europe/Moscow","venue":"Москва","full_price":4000,"child_price":1500,"capacity":20}`
	w := call("POST", "/api/admin/events", body, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var e domain.Event
	if err = json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/events/" + e.ID
	if e.ID == "" || w.Header().Get("Location") != path {
		t.Fatal("missing UUID/location")
	}
	if call("GET", path, "", true).Code != 200 {
		t.Fatal("get")
	}
	w = call("PATCH", path, `{"title":"Изменено","child_price":1700}`, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var patched domain.Event
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Title != "Изменено" || patched.ChildPrice != 1700 || patched.Capacity != 20 || patched.Description != "Описание" {
		t.Fatal("patch overwrote unrelated data")
	}
	for _, body := range []string{`{"remaining":100}`, `{"id":"other"}`, `{"title":null}`, `{"capacity":2.5}`, `{} {}`} {
		if call("PATCH", path, body, true).Code != 400 {
			t.Fatal("invalid patch accepted", body)
		}
	}
	if call("PATCH", path, `{"timezone":"Local"}`, true).Code != 422 {
		t.Fatal("server-local timezone accepted")
	}
	if call("PATCH", path, `{"timezone":"Invalid/Zone"}`, true).Code != 422 {
		t.Fatal("timezone")
	}
	if call("PATCH", path, `{"cover_url":"javascript:alert(1)"}`, true).Code != 422 {
		t.Fatal("image URL")
	}
	if call("PUT", path, `{"title":"Missing fields"}`, true).Code != 422 {
		t.Fatal("partial PUT accepted")
	}
	if call("PUT", path, body, true).Code != 200 {
		t.Fatal("full PUT")
	}
	if call("GET", "/api/admin/events/not-a-uuid", "", true).Code != 404 {
		t.Fatal("invalid id")
	}
	if call("DELETE", path, "{}", true).Code != 204 {
		t.Fatal("delete")
	}
	if call("GET", path, "", true).Code != 404 {
		t.Fatal("deleted read")
	}
	if call("DELETE", path, "{}", true).Code != 404 {
		t.Fatal("deleted twice")
	}
	list, err := events.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatal("delete not reflected in list")
	}
}
