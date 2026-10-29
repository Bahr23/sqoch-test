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
	"time"
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
		{`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":18}]}`, 201},
		{`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":17}]}`, 422},
		{`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":18.5}]}`, 400},
		{`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":18}],"total":1}`, 400},
		{`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":18}]} {}`, 400},
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("POST", "/api/checkouts", strings.NewReader(tc.body)))
		if r.Code != tc.status {
			t.Fatalf("expected %d got %d %s", tc.status, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("POST", "/api/checkouts", strings.NewReader(`{"event_id":"b9264c68-9de5-4aa1-83bd-9b3e6fa72cf1","email":"a@b.ru","participants":[{"name":"Анна","age":18},{"name":"Маша","age":13}],"simulate_sold_out":true}`)))
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
	if _, err := sqlite.SeedDemoOrders(ctx, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, httptest.NewRequest("POST", "/api/checkouts/demo-orders-v1-1-03-checkout/confirm", nil))
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(cancelled.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if cancelled.Code != 409 || problem.Code != "cancelled" {
		t.Fatalf("cancelled order returned as success: %d %s", cancelled.Code, cancelled.Body.String())
	}
}
