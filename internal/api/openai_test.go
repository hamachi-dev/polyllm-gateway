package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"github.com/hamachi-dev/polyllm-gateway/internal/model"
	"github.com/hamachi-dev/polyllm-gateway/internal/provider"
	"github.com/hamachi-dev/polyllm-gateway/internal/resolver"
	"strings"
	"testing"
)

type mockProvider struct {
	resp *model.ChatResponse
	err  error
}

func (m *mockProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	return m.resp, m.err
}

type mockStreamProvider struct {
	body io.ReadCloser
}

func (m *mockStreamProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	return &model.ChatResponse{
		Model:      req.Model,
		StreamBody: m.body,
	}, nil
}

func TestOpenAIHandlerNonStreaming(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{
		"gpt-5": {Provider: "test", Model: "qwen3.7-plus", API: "openai"},
	})

	p := &mockProvider{
		resp: &model.ChatResponse{
			Model: "qwen3.7-plus",
			Message: model.Message{
				Role:    "assistant",
				Content: "Hello!",
			},
			FinishReason: "stop",
			Usage: &model.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		},
	}

	providers := map[string]provider.Provider{"test": p}
	log := discardLog()
	handler := NewOpenAIHandler(res, providers, log)

	body := map[string]interface{}{
		"model": "gpt-5",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	choices := resp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"] != "Hello!" {
		t.Errorf("expected Hello!, got %v", msg["content"])
	}
	if msg["role"] != "assistant" {
		t.Errorf("expected assistant, got %v", msg["role"])
	}
}

func TestOpenAIHandlerStreaming(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{
		"gpt-5": {Provider: "test", Model: "qwen3.7-plus", API: "openai"},
	})

	streamBody := io.NopCloser(strings.NewReader(
		`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: [DONE]
`,
	))
	p := &mockStreamProvider{body: streamBody}
	providers := map[string]provider.Provider{"test": p}
	log := discardLog()
	handler := NewOpenAIHandler(res, providers, log)

	body := map[string]interface{}{
		"model":    "gpt-5",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"stream":   true,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	if !strings.Contains(w.Body.String(), "Hello") {
		t.Errorf("expected Hello in body, got: %s", w.Body.String())
	}
}

func TestOpenAIHandlerUnknownModel(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{})
	providers := map[string]provider.Provider{}
	log := discardLog()
	handler := NewOpenAIHandler(res, providers, log)

	body := map[string]interface{}{
		"model":    "unknown",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if !strings.Contains(errObj["message"].(string), "unknown") {
		t.Errorf("expected unknown model error, got %v", errObj)
	}
}

func TestOpenAIHandlerEmptyModel(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{})
	log := discardLog()
	handler := NewOpenAIHandler(res, map[string]provider.Provider{}, log)

	body := map[string]interface{}{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
