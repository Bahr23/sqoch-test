package config

import (
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AdminEmail    string
	AdminPassword string
	JWTSecret     string
	JWTTTL        time.Duration
	AdminOrigin   string
	DemoMode      bool
	Port          string
	SQLitePath    string
	LogLevel      slog.Level
}

func Load() (Config, error) {
	c := Config{Port: os.Getenv("API_PORT"), SQLitePath: os.Getenv("SQLITE_PATH")}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("API_PORT must be a port between 1 and 65535")
	}
	if c.SQLitePath == "" || !filepath.IsAbs(c.SQLitePath) {
		return c, fmt.Errorf("SQLITE_PATH must be an absolute file path")
	}
	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		return c, fmt.Errorf("LOG_LEVEL is required")
	}
	if err := c.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return c, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	demo, err := strconv.ParseBool(os.Getenv("DEMO_MODE"))
	if err != nil {
		return c, fmt.Errorf("DEMO_MODE must be true or false")
	}
	c.DemoMode = demo
	c.AdminEmail = strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL")))
	address, err := mail.ParseAddress(c.AdminEmail)
	if err != nil || address.Address != c.AdminEmail {
		return c, fmt.Errorf("ADMIN_EMAIL must be an email address")
	}
	c.AdminPassword = os.Getenv("ADMIN_PASSWORD")
	if len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 {
		return c, fmt.Errorf("ADMIN_PASSWORD must contain 12 to 72 bytes")
	}
	c.JWTSecret = os.Getenv("JWT_SECRET")
	if len(c.JWTSecret) < 32 {
		return c, fmt.Errorf("JWT_SECRET must contain at least 32 bytes")
	}
	c.JWTTTL, err = time.ParseDuration(os.Getenv("JWT_TTL"))
	if err != nil || c.JWTTTL < time.Minute || c.JWTTTL > 24*time.Hour {
		return c, fmt.Errorf("JWT_TTL must be between 1m and 24h")
	}
	c.AdminOrigin = os.Getenv("ADMIN_ORIGIN")
	origin, err := url.Parse(c.AdminOrigin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return c, fmt.Errorf("ADMIN_ORIGIN must be an http(s) origin without a path")
	}
	host := os.Getenv("ADMIN_HOST")
	if host != origin.Hostname() || !strings.HasPrefix(host, "admin.") || strings.ContainsAny(host, " \t\n;{}$") {
		return c, fmt.Errorf("ADMIN_HOST must match the admin subdomain in ADMIN_ORIGIN")
	}
	if origin.Scheme == "http" && host != "admin.localhost" {
		return c, fmt.Errorf("non-local ADMIN_ORIGIN requires HTTPS")
	}
	return c, nil
}
