package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Assistant struct {
	Key, Model          string
	Timeout             time.Duration
	MaxSteps, MaxTokens int
}

func LoadAssistant() (Assistant, error) {
	c := Assistant{Key: strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")), Model: strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")), Timeout: 120 * time.Second, MaxSteps: 8, MaxTokens: 4096}
	var err error
	if v := os.Getenv("AGENT_TIMEOUT"); v != "" {
		c.Timeout, err = time.ParseDuration(v)
		if err != nil || c.Timeout < 10*time.Second || c.Timeout > 10*time.Minute {
			return c, fmt.Errorf("AGENT_TIMEOUT must be between 10s and 10m")
		}
	}
	for _, p := range []struct {
		name     string
		target   *int
		min, max int
	}{{"AGENT_MAX_STEPS", &c.MaxSteps, 1, 20}, {"AGENT_MAX_TOKENS", &c.MaxTokens, 256, 16000}} {
		if v := os.Getenv(p.name); v != "" {
			*p.target, err = strconv.Atoi(v)
			if err != nil || *p.target < p.min || *p.target > p.max {
				return c, fmt.Errorf("%s is outside its permitted range", p.name)
			}
		}
	}
	return c, nil
}
