package http_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	delivery "sqoch-ticket/backend/internal/delivery/http"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		closed bool
		code   int
		body   string
	}{
		{"available database", false, 200, `{"status":"ok","database":"ok"}`},
		{"unavailable database", true, 503, `{"status":"error","database":"error"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if tc.closed {
				db.Close()
			}
			handler := delivery.NewHandler(usecase.NewHealth(db), slog.New(slog.NewTextHandler(io.Discard, nil)))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/health", nil))
			if response.Code != tc.code || strings.TrimSpace(response.Body.String()) != tc.body {
				t.Fatalf("got %d %s", response.Code, response.Body.String())
			}
		})
	}
}
