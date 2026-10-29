package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("unauthorized")

type AdminSession struct {
	ID        string    `json:"-"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}
type AdminSessions interface {
	Create(context.Context, AdminSession) error
	Active(context.Context, AdminSession) (bool, error)
	Delete(context.Context, string) error
}
type AdminTokens interface {
	Sign(AdminSession) (string, error)
	Verify(string) (AdminSession, error)
}
type PasswordVerifier interface{ Matches(string) bool }
type Auth struct {
	email     string
	passwords PasswordVerifier
	tokens    AdminTokens
	sessions  AdminSessions
	ttl       time.Duration
}

func NewAuth(email string, passwords PasswordVerifier, tokens AdminTokens, sessions AdminSessions, ttl time.Duration) *Auth {
	return &Auth{email: strings.ToLower(email), passwords: passwords, tokens: tokens, sessions: sessions, ttl: ttl}
}
func (a *Auth) Login(ctx context.Context, email, password string) (string, AdminSession, error) {
	// Always verify the password, including for an unknown email.
	validPassword := a.passwords.Matches(password)
	expected, actual := sha256.Sum256([]byte(a.email)), sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 || !validPassword {
		return "", AdminSession{}, ErrUnauthorized
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return "", AdminSession{}, err
	}
	session := AdminSession{ID: hex.EncodeToString(id), Email: a.email, ExpiresAt: time.Now().Add(a.ttl).UTC().Truncate(time.Second)}
	token, err := a.tokens.Sign(session)
	if err != nil {
		return "", AdminSession{}, err
	}
	if err := a.sessions.Create(ctx, session); err != nil {
		return "", AdminSession{}, err
	}
	return token, session, nil
}
func (a *Auth) Authenticate(ctx context.Context, token string) (AdminSession, error) {
	session, err := a.tokens.Verify(token)
	if err != nil || session.Email != a.email {
		return AdminSession{}, ErrUnauthorized
	}
	active, err := a.sessions.Active(ctx, session)
	if err != nil {
		return AdminSession{}, err
	}
	if !active {
		return AdminSession{}, ErrUnauthorized
	}
	return session, nil
}
func (a *Auth) Logout(ctx context.Context, token string) error {
	session, err := a.tokens.Verify(token)
	if err != nil {
		return nil
	}
	return a.sessions.Delete(ctx, session.ID)
}
