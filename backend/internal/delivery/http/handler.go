package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type HealthChecker interface{ Check(context.Context) error }

func NewHandler(health HealthChecker, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		result := struct {
			Status   string `json:"status"`
			Database string `json:"database"`
		}{"ok", "ok"}
		if err := health.Check(ctx); err != nil {
			logger.Error("database health check failed", "error", err)
			result.Status, result.Database = "error", "error"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	return mux
}
