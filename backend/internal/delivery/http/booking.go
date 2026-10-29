package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sqoch-ticket/backend/internal/domain"
	"sqoch-ticket/backend/internal/usecase"
)

func NewBookingHandler(health HealthChecker, booking *usecase.Booking, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/health", NewHandler(health, logger))
	mux.HandleFunc("GET /api/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		event, err := booking.Event(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, http.StatusOK, event)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := booking.Events(r.Context())
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, 200, events)
	})
	mux.HandleFunc("POST /api/checkouts", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input domain.OrderInput
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Некорректные данные заказа."})
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Ожидается один JSON-объект."})
			return
		}
		checkout, err := booking.Checkout(r.Context(), input)
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, http.StatusCreated, checkout)
	})
	mux.HandleFunc("POST /api/checkouts/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		order, err := booking.Confirm(r.Context(), r.PathValue("id"))
		if err != nil {
			bookingError(w, err, logger)
			return
		}
		writeJSON(w, http.StatusOK, order)
	})
	return mux
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func bookingError(w http.ResponseWriter, err error, logger *slog.Logger) {
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		writeJSON(w, 422, map[string]any{"code": "validation", "message": "Проверьте введённые данные.", "fields": validation.Fields})
		return
	}
	switch {
	case errors.Is(err, domain.ErrMailingLocked):
		writeJSON(w, 409, map[string]string{"code": "mailing_locked", "message": "Рассылка уже запущена. Её текст и исходный список получателей зафиксированы."})
	case errors.Is(err, domain.ErrMailingChanged):
		writeJSON(w, 409, map[string]string{"code": "mailing_changed", "message": "Текст или список получателей изменился. Обновите данные и подтвердите запуск ещё раз."})
	case errors.Is(err, domain.ErrCancelled):
		writeJSON(w, 409, map[string]string{"code": "cancelled", "message": "Этот заказ отменён. Для покупки билетов оформите новый заказ."})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, 409, map[string]string{"code": "conflict", "message": "Вместимость не может быть меньше числа проданных билетов."})
	case errors.Is(err, domain.ErrSoldOut):
		writeJSON(w, 409, map[string]string{"code": "sold_out", "message": "Для этого заказа больше нет мест. Заказ не оформлен."})
	case errors.Is(err, domain.ErrExpired):
		writeJSON(w, 410, map[string]string{"code": "expired", "message": "Время проверки заказа истекло. Проверьте данные ещё раз."})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, 404, map[string]string{"code": "not_found", "message": "Запись не найдена или больше недоступна."})
	default:
		logger.Error("booking operation failed", "error", err)
		writeJSON(w, 503, map[string]string{"code": "unavailable", "message": "Сервис временно недоступен. Попробуйте ещё раз."})
	}
}
