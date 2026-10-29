package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sqoch-ticket/backend/internal/infrastructure/rabbit"
	"strconv"
	"strings"
	"time"
)

type Messaging struct {
	Rabbit rabbit.Config
	Token  string
}

func LoadMessaging() (Messaging, error) {
	c := Messaging{Rabbit: rabbit.Config{URL: os.Getenv("RABBITMQ_URL"), Commands: os.Getenv("RABBITMQ_COMMAND_QUEUE"), Results: os.Getenv("RABBITMQ_RESULT_QUEUE")}, Token: os.Getenv("NOTIFICATION_SERVICE_TOKEN")}
	u, err := url.Parse(c.Rabbit.URL)
	if err != nil || u.Host == "" || (u.Scheme != "amqp" && u.Scheme != "amqps") {
		return c, fmt.Errorf("RABBITMQ_URL must be an AMQP URL")
	}
	for _, name := range []string{c.Rabbit.Commands, c.Rabbit.Results} {
		if name == "" || len(name) > 200 || strings.HasPrefix(name, "amq.") {
			return c, fmt.Errorf("invalid RabbitMQ queue name")
		}
	}
	if c.Rabbit.Commands == c.Rabbit.Results {
		return c, fmt.Errorf("command and result queues must differ")
	}
	if len(c.Token) < 32 {
		return c, fmt.Errorf("NOTIFICATION_SERVICE_TOKEN must contain at least 32 bytes")
	}
	c.Rabbit.Interval, err = duration("OUTBOX_INTERVAL", 50*time.Millisecond, time.Minute)
	return c, err
}

type Notifier struct {
	Messaging                                                     Messaging
	Port, SQLitePath, MockSQLitePath, BackendURL, MockFailureMode string
	Interval, Lease, Timeout, RetryBase, MockDelay                time.Duration
	MaxAttempts                                                   int
}

func LoadNotifier() (Notifier, error) {
	var c Notifier
	var err error
	if c.Messaging, err = LoadMessaging(); err != nil {
		return c, err
	}
	c.Port = os.Getenv("NOTIFIER_PORT")
	port, e := strconv.Atoi(c.Port)
	if e != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("invalid NOTIFIER_PORT")
	}
	c.SQLitePath = os.Getenv("NOTIFIER_SQLITE_PATH")
	c.MockSQLitePath = os.Getenv("MOCK_SQLITE_PATH")
	if !filepath.IsAbs(c.SQLitePath) || !filepath.IsAbs(c.MockSQLitePath) || filepath.Clean(c.SQLitePath) == filepath.Clean(c.MockSQLitePath) {
		return c, fmt.Errorf("notifier and mock SQLite paths must be distinct absolute paths")
	}
	c.BackendURL = os.Getenv("BACKEND_INTERNAL_URL")
	u, e := url.Parse(c.BackendURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return c, fmt.Errorf("invalid BACKEND_INTERNAL_URL")
	}
	c.MockFailureMode = os.Getenv("MOCK_FAILURE_MODE")
	switch c.MockFailureMode {
	case "none", "temporary_once", "after_accept_once", "always_fail":
	default:
		return c, fmt.Errorf("invalid MOCK_FAILURE_MODE")
	}
	for _, v := range []struct {
		name     string
		value    *time.Duration
		min, max time.Duration
	}{
		{"NOTIFIER_INTERVAL", &c.Interval, 50 * time.Millisecond, time.Minute}, {"NOTIFIER_LEASE", &c.Lease, time.Second, 10 * time.Minute}, {"NOTIFIER_TIMEOUT", &c.Timeout, 100 * time.Millisecond, time.Minute}, {"NOTIFIER_RETRY_BASE", &c.RetryBase, 100 * time.Millisecond, time.Hour}, {"MOCK_SEND_DELAY", &c.MockDelay, 0, time.Minute},
	} {
		*v.value, err = duration(v.name, v.min, v.max)
		if err != nil {
			return c, err
		}
	}
	if c.Lease < 4*c.Timeout {
		return c, fmt.Errorf("NOTIFIER_LEASE must be at least four request timeouts")
	}
	if c.MockDelay >= c.Timeout {
		return c, fmt.Errorf("MOCK_SEND_DELAY must be less than NOTIFIER_TIMEOUT")
	}
	c.MaxAttempts, e = strconv.Atoi(os.Getenv("NOTIFIER_MAX_ATTEMPTS"))
	if e != nil || c.MaxAttempts < 1 || c.MaxAttempts > 20 {
		return c, fmt.Errorf("NOTIFIER_MAX_ATTEMPTS must be 1..20")
	}
	return c, nil
}
func duration(key string, min, max time.Duration) (time.Duration, error) {
	v, e := time.ParseDuration(os.Getenv(key))
	if e != nil || v < min || v > max {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return v, nil
}
