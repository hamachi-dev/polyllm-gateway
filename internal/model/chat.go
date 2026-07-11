package model

import (
	"encoding/json"
	"io"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function FunctionCall    `json:"function"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatRequest struct {
	Model       string
	Messages    []Message
	Stream      bool
	Temperature *float64
	MaxTokens   *int
	Stop        []string
	API         string
	Tools       []ToolDef
	ToolChoice  string
}

type ChatResponse struct {
	Model        string
	Message      Message
	FinishReason string
	Usage        *Usage
	StreamBody   io.ReadCloser
}
