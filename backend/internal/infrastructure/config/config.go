package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	DemoMode   bool
	Port       string
	SQLitePath string
	LogLevel   slog.Level
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
	return c, nil
}
