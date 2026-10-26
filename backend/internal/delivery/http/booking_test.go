package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	delivery "sqoch-ticket/backend/internal/delivery/http"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
)

func TestBookingAPI(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	handler := delivery.NewBookingHandler(usecase.NewHealth(db), usecase.NewBooking(sqlite.NewBookingRepository(db), true), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"email":"a@b.ru","participants":[{"name":"Анна","age":18}]}`, 201},
		{`{"email":"a@b.ru","participants":[{"name":"Анна","age":17}]}`, 422},
		{`{"email":"a@b.ru","participants":[{"name":"Анна","age":18.5}]}`, 400},
		{`{"email":"a@b.ru","participants":[{"name":"Анна","age":18}],"total":1}`, 400},
		{`{"email":"a@b.ru","participants":[{"name":"Анна","age":18}]} {}`, 400},
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("POST", "/api/checkouts", strings.NewReader(tc.body)))
		if r.Code != tc.status {
			t.Fatalf("expected %d got %d %s", tc.status, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("POST", "/api/checkouts", strings.NewReader(`{"email":"a@b.ru","participants":[{"name":"Анна","age":18},{"name":"Маша","age":13}],"simulate_sold_out":true}`)))
	var q domain.Checkout
	if err := json.Unmarshal(r.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if r.Code != 201 || q.Total != 3000 {
		t.Fatalf("checkout %d %s", r.Code, r.Body.String())
	}
	confirm := httptest.NewRecorder()
	handler.ServeHTTP(confirm, httptest.NewRequest("POST", "/api/checkouts/"+q.ID+"/confirm", nil))
	if confirm.Code != 409 {
		t.Fatalf("expected conflict, got %d", confirm.Code)
	}
}
