package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sqoch-ticket/backend/internal/domain"
)

// Endpoint is fixed by the application. Only tests substitute a local server.
type Client struct {
	Key, Model, Endpoint string
	MaxTokens            int
	HTTP                 *http.Client
}

func (c *Client) Complete(ctx context.Context, messages []domain.AIMessage, tools []any) (domain.AIMessage, error) {
	// The worker executes tool batches sequentially. Do not require the optional
	// parallel_tool_calls parameter: some tool-capable providers do not support it
	// and require_parameters would exclude all of their endpoints.
	input := map[string]any{"model": c.Model, "messages": messages, "tools": tools, "max_tokens": c.MaxTokens, "provider": map[string]bool{"require_parameters": true}}
	b, err := json.Marshal(input)
	if err != nil {
		return domain.AIMessage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(b))
	if err != nil {
		return domain.AIMessage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "Sqoch Ticket")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return domain.AIMessage{}, fmt.Errorf("OpenRouter connection failed")
	}
	defer res.Body.Close()
	// Never return or log provider response bodies: they may contain prompt data.
	if res.StatusCode != 200 {
		return domain.AIMessage{}, fmt.Errorf("OpenRouter HTTP %d", res.StatusCode)
	}
	var output struct {
		Choices []struct {
			Message      domain.AIMessage `json:"message"`
			FinishReason string           `json:"finish_reason"`
		} `json:"choices"`
		Error json.RawMessage `json:"error"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&output); err != nil || len(output.Choices) != 1 || (len(output.Error) > 0 && string(output.Error) != "null") {
		return domain.AIMessage{}, fmt.Errorf("invalid OpenRouter response")
	}
	m := output.Choices[0].Message
	if output.Choices[0].FinishReason == "error" {
		return m, fmt.Errorf("OpenRouter generation failed")
	}
	if output.Choices[0].FinishReason == "length" {
		return m, fmt.Errorf("OpenRouter response exceeded token limit")
	}
	if m.Role != "assistant" || (m.Content == "" && len(m.ToolCalls) == 0) || len(m.ToolCalls) > 8 {
		return m, fmt.Errorf("invalid assistant response")
	}
	ids := map[string]bool{}
	for _, call := range m.ToolCalls {
		if call.ID == "" || len(call.ID) > 200 || call.Type != "function" || ids[call.ID] || len(call.Function.Arguments) > 64000 {
			return m, fmt.Errorf("invalid tool call")
		}
		ids[call.ID] = true
	}
	return m, nil
}
