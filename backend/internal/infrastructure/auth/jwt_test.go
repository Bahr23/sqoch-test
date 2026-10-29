package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"sqoch-ticket/backend/internal/usecase"
	"strings"
	"testing"
	"time"
)

func TestJWTValidation(t *testing.T) {
	secret := strings.Repeat("s", 32)
	tokens := NewTokens(secret)
	s := usecase.AdminSession{ID: "test-session", Email: "organizer@example.com", ExpiresAt: time.Now().Add(time.Hour)}
	valid, err := tokens.Sign(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.Verify(valid); err != nil {
		t.Fatal(err)
	}
	if _, err = NewTokens(strings.Repeat("x", 32)).Verify(valid); err == nil {
		t.Fatal("wrong signing key accepted")
	}
	for _, variant := range []string{"expired", "no-exp", "issuer", "audience", "role", "future", "algorithm"} {
		t.Run(variant, func(t *testing.T) {
			c := claims{Role: "organizer", RegisteredClaims: jwt.RegisteredClaims{ID: s.ID, Subject: s.Email, Issuer: "sqoch-ticket", Audience: jwt.ClaimStrings{"sqoch-admin"}, ExpiresAt: jwt.NewNumericDate(s.ExpiresAt), IssuedAt: jwt.NewNumericDate(time.Now())}}
			method := jwt.SigningMethodHS256
			switch variant {
			case "expired":
				c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
			case "no-exp":
				c.ExpiresAt = nil
			case "issuer":
				c.Issuer = "other"
			case "audience":
				c.Audience = jwt.ClaimStrings{"other"}
			case "role":
				c.Role = "customer"
			case "future":
				c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
			case "algorithm":
				method = jwt.SigningMethodHS384
			}
			raw, err := jwt.NewWithClaims(method, c).SignedString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tokens.Verify(raw); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	if _, err = tokens.Verify("garbage"); err == nil {
		t.Fatal("malformed token accepted")
	}
}
