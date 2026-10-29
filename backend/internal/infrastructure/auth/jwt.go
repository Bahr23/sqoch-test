package auth

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"sqoch-ticket/backend/internal/usecase"
	"time"
)

type Password struct{ hash []byte }

func NewPassword(value string) (*Password, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	return &Password{hash: hash}, err
}
func (p *Password) Matches(value string) bool {
	return bcrypt.CompareHashAndPassword(p.hash, []byte(value)) == nil
}

type Tokens struct{ secret []byte }

func NewTokens(secret string) *Tokens { return &Tokens{secret: []byte(secret)} }

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (t *Tokens) Sign(s usecase.AdminSession) (string, error) {
	c := claims{Role: "organizer", RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "sqoch-ticket", Subject: s.Email, Audience: jwt.ClaimStrings{"sqoch-admin"}, ID: s.ID,
		IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(s.ExpiresAt),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
}
func (t *Tokens) Verify(raw string) (usecase.AdminSession, error) {
	c := new(claims)
	token, err := jwt.ParseWithClaims(raw, c, func(token *jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("sqoch-ticket"), jwt.WithAudience("sqoch-admin"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid || c.Role != "organizer" || c.ID == "" || c.Subject == "" || c.ExpiresAt == nil {
		return usecase.AdminSession{}, fmt.Errorf("invalid admin token")
	}
	return usecase.AdminSession{ID: c.ID, Email: c.Subject, ExpiresAt: c.ExpiresAt.Time}, nil
}
