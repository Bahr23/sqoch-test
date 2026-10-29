package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/usecase"
)

type organizerKey struct{}

func withOrganizer(r *http.Request, email string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), organizerKey{}, email))
}
func NewAssistantHandler(a *usecase.Assistant, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, err error) {
		status := 409
		message := ""
		switch {
		case errors.Is(err, domain.ErrChatBusy):
			message = "Сначала дождитесь ответа или повторите незавершённый запрос."
		case errors.Is(err, domain.ErrChatLimit):
			message = "Достигнут лимит длины диалога. Начните новый диалог."
		case errors.Is(err, domain.ErrProposalStale):
			message = "Ивент изменился после подготовки предложения. Отклоните его и попросите помощника подготовить новое."
		case errors.Is(err, domain.ErrAssistantDisabled):
			status = 503
			message = "Настройте OPENROUTER_API_KEY и OPENROUTER_MODEL в .env, затем пересоздайте backend."
		default:
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, status, map[string]string{"message": message})
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		r.Body = http.MaxBytesReader(w, r.Body, 20000)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
			writeJSON(w, 400, map[string]string{"message": "Проверьте данные запроса."})
			return false
		}
		return true
	}
	owner := func(r *http.Request) string { v, _ := r.Context().Value(organizerKey{}).(string); return v }
	mux.HandleFunc("GET /api/admin/assistant/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"enabled": a.Enabled, "model": a.ModelName})
	})
	mux.HandleFunc("GET /api/admin/assistant/threads", func(w http.ResponseWriter, r *http.Request) {
		v, err := a.Repo.Threads(r.Context(), owner(r))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/admin/assistant/threads", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &v) {
			return
		}
		if _, err := uuid.Parse(v.ID); err != nil {
			writeJSON(w, 400, map[string]string{"message": "Некорректный ID диалога."})
			return
		}
		t, err := a.Repo.CreateThread(r.Context(), owner(r), v.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, t)
	})
	mux.HandleFunc("GET /api/admin/assistant/threads/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, err := a.Repo.Details(r.Context(), owner(r), r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		for i := range d.Messages {
			d.Messages[i].ReasoningDetails = nil
		}
		writeJSON(w, 200, d)
	})
	mux.HandleFunc("POST /api/admin/assistant/threads/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if !decode(w, r, &v) {
			return
		}
		if err := a.Send(r.Context(), owner(r), r.PathValue("id"), v.ID, v.Message); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 202, map[string]string{"id": v.ID})
	})
	mux.HandleFunc("POST /api/admin/assistant/threads/{id}/runs/{run}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled {
			fail(w, domain.ErrAssistantDisabled)
			return
		}
		if err := a.Repo.Retry(r.Context(), owner(r), r.PathValue("id"), r.PathValue("run")); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 202, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/admin/assistant/threads/{id}/proposals/{proposal}/decision", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Action string `json:"action"`
		}
		if !decode(w, r, &v) {
			return
		}
		if v.Action != "apply" && v.Action != "reject" {
			writeJSON(w, 400, map[string]string{"message": "Выберите подтверждение или отклонение."})
			return
		}
		p, err := a.Repo.Decide(r.Context(), owner(r), r.PathValue("id"), r.PathValue("proposal"), v.Action == "apply")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, p)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if owner(r) == "" {
			writeJSON(w, 401, map[string]string{"message": "Войдите в панель организатора."})
			return
		}
		mux.ServeHTTP(w, r)
	})
}
