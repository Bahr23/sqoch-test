package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"time"
)

const adminCookie = "sqoch_admin"

func NewAdminHandler(auth *usecase.Auth, origin string, logger *slog.Logger, protected ...http.Handler) http.Handler {
	mux := http.NewServeMux()
	secure := strings.HasPrefix(origin, "https://")
	cookie := func(w http.ResponseWriter, token string, expires time.Time, age int) {
		http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/api/admin", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: age})
	}
	tokenFrom := func(r *http.Request) string {
		c, err := r.Cookie(adminCookie)
		if err != nil {
			return ""
		}
		return c.Value
	}
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, usecase.ErrUnauthorized) {
			writeJSON(w, 401, map[string]string{"code": "unauthorized", "message": "Войдите в панель организатора."})
			return
		}
		logger.Error("admin authentication failed", "error", err)
		writeJSON(w, 503, map[string]string{"code": "unavailable", "message": "Сервис временно недоступен. Попробуйте снова."})
	}
	mux.HandleFunc("POST /api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(&struct{}{}) != io.EOF || len(input.Email) > 254 || len(input.Password) > 72 || input.Password == "" {
			writeJSON(w, 400, map[string]string{"code": "invalid_request", "message": "Укажите email и пароль."})
			return
		}
		token, s, err := auth.Login(r.Context(), input.Email, input.Password)
		if errors.Is(err, usecase.ErrUnauthorized) {
			writeJSON(w, 401, map[string]string{"code": "invalid_credentials", "message": "Неверный email или пароль."})
			return
		}
		if err != nil {
			fail(w, err)
			return
		}
		// A fresh login replaces this browser's previous session.
		if err := auth.Logout(r.Context(), tokenFrom(r)); err != nil {
			fail(w, err)
			return
		}
		cookie(w, token, s.ExpiresAt, max(1, int(time.Until(s.ExpiresAt).Seconds())))
		writeJSON(w, 200, s)
	})
	mux.HandleFunc("GET /api/admin/me", func(w http.ResponseWriter, r *http.Request) {
		s, err := auth.Authenticate(r.Context(), tokenFrom(r))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, s)
	})
	mux.HandleFunc("POST /api/admin/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Logout(r.Context(), tokenFrom(r)); err != nil {
			fail(w, err)
			return
		}
		cookie(w, "", time.Unix(1, 0), -1)
		w.WriteHeader(http.StatusNoContent)
	})
	// Future organizer routes must go inside this protected subtree.
	mux.Handle("/api/admin/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := auth.Authenticate(r.Context(), tokenFrom(r))
		if err != nil {
			fail(w, err)
			return
		}
		if len(protected) > 0 {
			protected[0].ServeHTTP(w, withOrganizer(r, session.Email))
		} else {
			http.NotFound(w, r)
		}
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if r.Header.Get("Origin") != origin || contentType != "application/json" {
				writeJSON(w, 403, map[string]string{"code": "forbidden", "message": "Запрос отклонён. Откройте панель по её основному адресу."})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
