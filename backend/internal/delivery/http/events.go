package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/usecase"
)

func NewEventManagementHandler(events *usecase.Events, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/events", func(w http.ResponseWriter, r *http.Request) {
		list, err := events.List(r.Context())
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, list)
	})
	mux.HandleFunc("GET /api/admin/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		event, err := events.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, event)
	})
	save := func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		d := json.NewDecoder(r.Body)
		var fields map[string]json.RawMessage
		if d.Decode(&fields) != nil || fields == nil || d.Decode(&struct{}{}) != io.EOF {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Ожидается один JSON-объект."})
			return
		}
		input := domain.EventInput{}
		if r.Method == http.MethodPatch {
			current, err := events.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				bookingError(w, err, logger)
				return
			}
			input = current.Input()
		}
		encoded, _ := json.Marshal(input)
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &merged)
		for key, value := range fields {
			if _, ok := merged[key]; !ok || string(value) == "null" {
				writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Неизвестное поле или null: " + key})
				return
			}
			merged[key] = value
		}
		encoded, _ = json.Marshal(merged)
		if json.Unmarshal(encoded, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Проверьте типы полей мероприятия."})
			return
		}
		result, err := events.Save(r.Context(), r.PathValue("id"), input)
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		status := 200
		if r.Method == http.MethodPost {
			status = 201
			w.Header().Set("Location", "/api/admin/events/"+result.ID)
		}
		writeJSON(w, status, result)
	}
	mux.HandleFunc("POST /api/admin/events", save)
	mux.HandleFunc("PUT /api/admin/events/{id}", save)
	mux.HandleFunc("PATCH /api/admin/events/{id}", save)
	mux.HandleFunc("DELETE /api/admin/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := events.Delete(r.Context(), r.PathValue("id")); err != nil {
			bookingError(w, err, logger)
			return
		}
		w.WriteHeader(204)
	})
	return mux
}
