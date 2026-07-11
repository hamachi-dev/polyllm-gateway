package model

import (
	"io"
	"strings"
	"testing"
)

func TestMessage(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	if m.Role != "user" {
		t.Errorf("expected user, got %s", m.Role)
	}
	if m.Content != "hello" {
		t.Errorf("expected hello, got %s", m.Content)
	}
}

func TestUsage(t *testing.T) {
	u := Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
	if u.TotalTokens != 30 {
		t.Errorf("expected 30, got %d", u.TotalTokens)
	}
}

func TestChatRequest(t *testing.T) {
	token := 100
	temp := 0.5
	req := ChatRequest{
		Model:       "test-model",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Stream:      true,
		Temperature: &temp,
		MaxTokens:   &token,
		Stop:        []string{".", "!"},
		API:         "openai",
	}

	if req.Model != "test-model" {
		t.Errorf("expected test-model, got %s", req.Model)
	}
	if !req.Stream {
		t.Error("expected stream true")
	}
	if *req.Temperature != 0.5 {
		t.Errorf("expected 0.5, got %f", *req.Temperature)
	}
	if req.API != "openai" {
		t.Errorf("expected openai, got %s", req.API)
	}
}

func TestChatRequestAnthropicAPI(t *testing.T) {
	req := ChatRequest{
		Model: "claude-model",
		API:   "anthropic",
	}
	if req.API != "anthropic" {
		t.Errorf("expected anthropic, got %s", req.API)
	}
}

func TestChatResponse(t *testing.T) {
	resp := ChatResponse{
		Model: "test",
		Message: Message{
			Role:    "assistant",
			Content: "hello",
		},
		FinishReason: "stop",
	}

	if resp.Message.Content != "hello" {
		t.Errorf("expected hello, got %s", resp.Message.Content)
	}
}

func TestChatResponseStreamBody(t *testing.T) {
	body := io.NopCloser(strings.NewReader("stream data"))
	resp := ChatResponse{
		Model:      "test",
		StreamBody: body,
	}

	if resp.StreamBody == nil {
		t.Fatal("expected non-nil StreamBody")
	}

	data, _ := io.ReadAll(resp.StreamBody)
	resp.StreamBody.Close()

	if string(data) != "stream data" {
		t.Errorf("expected 'stream data', got '%s'", string(data))
	}
}
