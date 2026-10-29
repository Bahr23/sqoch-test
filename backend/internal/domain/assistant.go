package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrChatBusy = errors.New("chat has an unfinished request")
var ErrChatLimit = errors.New("chat limit reached")
var ErrProposalStale = errors.New("event changed since proposal")
var ErrAssistantDisabled = errors.New("assistant is not configured")

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type AIMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Some OpenRouter providers require these opaque fields on the next turn.
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}
type ChatMessage struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	CreatedAt string `json:"created_at"`
	AIMessage
}
type ChatRun struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
	Error    string `json:"error"`
}
type ChatThread struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}
type Proposal struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	RunID       string     `json:"run_id"`
	EventID     string     `json:"event_id"`
	Before      EventInput `json:"before"`
	After       EventInput `json:"after"`
	MailingBody string     `json:"mailing_body"`
	MailingID   string     `json:"mailing_id"`
	Status      string     `json:"status"`
}
type ChatDetails struct {
	ChatThread
	Messages  []ChatMessage `json:"messages"`
	Proposals []Proposal    `json:"proposals"`
	Runs      []ChatRun     `json:"runs"`
}

func EventFingerprint(v EventInput) string {
	b, _ := json.Marshal(v)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
