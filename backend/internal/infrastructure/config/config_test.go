package config

import "testing"

func TestAdminConfig(t *testing.T) {
	values := map[string]string{"API_PORT": "8081", "SQLITE_PATH": "/data/test.db", "LOG_LEVEL": "info", "DEMO_MODE": "true", "ADMIN_HOST": "admin.localhost", "ADMIN_ORIGIN": "http://admin.localhost:8080", "ADMIN_EMAIL": "organizer@example.com", "ADMIN_PASSWORD": "local-password-123", "JWT_SECRET": "01234567890123456789012345678901", "JWT_TTL": "1h"}
	for k, v := range values {
		t.Setenv(k, v)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{{"ADMIN_EMAIL", "bad"}, {"ADMIN_PASSWORD", "short"}, {"JWT_SECRET", "short"}, {"JWT_TTL", "0s"}, {"JWT_TTL", "48h"}, {"ADMIN_ORIGIN", "http://admin.localhost:8080/path"}, {"ADMIN_ORIGIN", "https://other.example"}, {"ADMIN_HOST", "bad; host"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
