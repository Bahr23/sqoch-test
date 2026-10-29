package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolCapableProviderWithoutParallelParameter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Reproduce OpenRouter's strict routing for providers supporting tools
		// but not the optional parallel_tool_calls parameter, even when false.
		if _, present := input["parallel_tool_calls"]; present {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"No endpoints found that can handle the requested parameters"}}`))
			return
		}
		var routing struct {
			RequireParameters bool `json:"require_parameters"`
		}
		if err := json.Unmarshal(input["provider"], &routing); err != nil || !routing.RequireParameters {
			t.Error("tool support must still be required")
		}
		if string(input["tools"]) == "null" || len(input["tools"]) == 0 {
			t.Error("tools were removed from the request")
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"read-1","type":"function","function":{"name":"read_event","arguments":"{}"}},{"id":"read-2","type":"function","function":{"name":"read_event","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer server.Close()
	c := Client{Key: "fixture", Model: "fixture", Endpoint: server.URL, HTTP: server.Client(), MaxTokens: 4096}
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "read_event", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}}
	message, err := c.Complete(context.Background(), nil, tools)
	if err != nil || len(message.ToolCalls) != 2 {
		t.Fatalf("provider without parallel option should still return tool batches: %v, %+v", err, message)
	}
}

func TestProviderFailuresDoNotLeakBodyOrKey(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{401, "secret provider message"}, {200, `{"error":{"message":"secret provider message"}}`}, {200, `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`}, {200, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"x","type":"function"},{"id":"x","type":"function"}]}}]}`}} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			c := Client{Key: "secret", Model: "fixture", Endpoint: server.URL, HTTP: server.Client()}
			_, err := c.Complete(context.Background(), nil, nil)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe provider error", err)
			}
		})
	}
}

func TestResponseWithNullErrorIsAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":null,"choices":[{"message":{"role":"assistant","content":"Готово"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	c := Client{Key: "fixture", Model: "fixture", Endpoint: server.URL, HTTP: server.Client()}
	reply, err := c.Complete(context.Background(), nil, nil)
	if err != nil || reply.Content != "Готово" {
		t.Fatal("valid completion rejected", err)
	}
}
