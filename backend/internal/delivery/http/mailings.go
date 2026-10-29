package http

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/usecase"
)

func NewOrganizerRoutes(events *usecase.Events, mailings *usecase.Mailings, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	eventRoutes := NewEventManagementHandler(events, logger)
	mailingRoutes := NewMailingHandler(mailings, logger)
	mux.Handle("/api/admin/events", eventRoutes)
	mux.Handle("/api/admin/events/", eventRoutes)
	mux.Handle("GET /api/admin/events/{id}/recipients", mailingRoutes)
	mux.Handle("/api/admin/mailings", mailingRoutes)
	mux.Handle("/api/admin/mailings/", mailingRoutes)
	return mux
}

func NewMailingHandler(mailings *usecase.Mailings, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/events/{id}/recipients", func(w http.ResponseWriter, r *http.Request) {
		values, err := mailings.Recipients(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, values)
	})
	mux.HandleFunc("GET /api/admin/mailings", func(w http.ResponseWriter, r *http.Request) {
		values, err := mailings.List(r.Context())
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, values)
	})
	mux.HandleFunc("GET /api/admin/mailings/{id}", func(w http.ResponseWriter, r *http.Request) {
		value, err := mailings.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, value)
	})
	save := func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		var input struct {
			EventID *string `json:"event_id"`
			Body    *string `json:"body"`
		}
		if d.Decode(&input) != nil || input.EventID == nil || input.Body == nil || d.Decode(&struct{}{}) != io.EOF {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Укажите ивент и текст в одном JSON-объекте."})
			return
		}
		value, err := mailings.Save(r.Context(), r.PathValue("id"), domain.MailingInput{EventID: *input.EventID, Body: *input.Body})
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		status := 200
		if r.Method == http.MethodPost {
			status = 201
			w.Header().Set("Location", "/api/admin/mailings/"+value.ID)
		}
		writeJSON(w, status, value)
	}
	mux.HandleFunc("POST /api/admin/mailings", save)
	mux.HandleFunc("PUT /api/admin/mailings/{id}", save)
	mux.HandleFunc("POST /api/admin/mailings/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		var input domain.MailingConfirmation
		if !decodeAction(w, r, &input) {
			return
		}
		value, err := mailings.Confirm(r.Context(), r.PathValue("id"), input)
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, value)
	})
	mux.HandleFunc("POST /api/admin/mailings/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decodeAction(w, r, &input) {
			return
		}
		value, err := mailings.Resume(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, value)
	})
	return mux
}

func decodeAction(w http.ResponseWriter, r *http.Request, input any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '{' || d.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Некорректные параметры действия."})
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(input) != nil {
		writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Некорректные параметры действия."})
		return false
	}
	return true
}

type EligibilityChecker interface {
	Eligible(context.Context, string) (bool, error)
}

func NewEligibilityHandler(checker EligibilityChecker, token string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/notifications/{id}/eligibility", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			writeJSON(w, 401, map[string]string{"code": "unauthorized"})
			return
		}
		eligible, err := checker.Eligible(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, map[string]bool{"eligible": eligible})
	})
	return mux
}
