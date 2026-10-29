package config

import "testing"

func TestNotifierConfiguration(t *testing.T) {
	values := map[string]string{
		"RABBITMQ_URL": "amqp://user:pass@rabbitmq:5672/", "RABBITMQ_COMMAND_QUEUE": "commands", "RABBITMQ_RESULT_QUEUE": "results", "NOTIFICATION_SERVICE_TOKEN": "01234567890123456789012345678901", "OUTBOX_INTERVAL": "250ms",
		"NOTIFIER_PORT": "8082", "NOTIFIER_SQLITE_PATH": "/data/jobs.db", "MOCK_SQLITE_PATH": "/data/mock.db", "BACKEND_INTERNAL_URL": "http://backend:8081", "MOCK_FAILURE_MODE": "none",
		"NOTIFIER_INTERVAL": "250ms", "NOTIFIER_LEASE": "30s", "NOTIFIER_TIMEOUT": "5s", "NOTIFIER_RETRY_BASE": "2s", "MOCK_SEND_DELAY": "1s", "NOTIFIER_MAX_ATTEMPTS": "3",
	}
	for k, v := range values {
		t.Setenv(k, v)
	}
	if _, err := LoadNotifier(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RABBITMQ_URL", "http://rabbitmq"}, {"RABBITMQ_RESULT_QUEUE", "commands"}, {"NOTIFICATION_SERVICE_TOKEN", "short"}, {"OUTBOX_INTERVAL", "0s"},
		{"NOTIFIER_PORT", "70000"}, {"NOTIFIER_SQLITE_PATH", "relative.db"}, {"MOCK_SQLITE_PATH", "/data/jobs.db"}, {"BACKEND_INTERNAL_URL", "ftp://backend"},
		{"MOCK_FAILURE_MODE", "smtp"}, {"NOTIFIER_LEASE", "5s"}, {"MOCK_SEND_DELAY", "5s"}, {"NOTIFIER_MAX_ATTEMPTS", "0"}, {"NOTIFIER_INTERVAL", "0s"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := LoadNotifier(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
