package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"github.com/taizo/polyllm-gateway/internal/model"
	"github.com/taizo/polyllm-gateway/internal/provider"
	"github.com/taizo/polyllm-gateway/internal/resolver"
	"strings"
	"testing"
)

func TestAnthropicHandlerNonStreaming(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{
		"claude-sonnet-4": {Provider: "test", Model: "deepseek-v4-flash", API: "anthropic"},
	})

	p := &mockProvider{
		resp: &model.ChatResponse{
			Model: "deepseek-v4-flash",
			Message: model.Message{
				Role:    "assistant",
				Content: "Hello from Anthropic!",
			},
			FinishReason: "end_turn",
			Usage: &model.Usage{
				PromptTokens:     5,
				CompletionTokens: 15,
				TotalTokens:      20,
			},
		},
	}

	providers := map[string]provider.Provider{"test": p}
	log := discardLog()
	handler := NewAnthropicHandler(res, providers, log)

	body := map[string]interface{}{
		"model":     "claude-sonnet-4",
		"messages":  []map[string]string{{"role": "user", "content": "hello"}},
		"max_tokens": 100,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(payload))
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

	if resp["type"] != "message" {
		t.Errorf("expected message type, got %v", resp["type"])
	}
	if resp["role"] != "assistant" {
		t.Errorf("expected assistant, got %v", resp["role"])
	}

	content := resp["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	block := content[0].(map[string]interface{})
	if block["text"] != "Hello from Anthropic!" {
		t.Errorf("expected Hello from Anthropic!, got %v", block["text"])
	}

	usage := resp["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(5) {
		t.Errorf("expected 5 input tokens, got %v", usage["input_tokens"])
	}
}

type flushableRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushableRecorder) Flush() error {
	f.ResponseRecorder.Flush()
	return nil
}

func TestAnthropicHandlerStreaming(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{
		"claude-sonnet-4": {Provider: "test", Model: "deepseek-v4-flash", API: "anthropic"},
	})

	streamBody := io.NopCloser(strings.NewReader(
		`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`,
	))
	p := &mockStreamProvider{body: streamBody}
	providers := map[string]provider.Provider{"test": p}
	log := discardLog()
	handler := NewAnthropicHandler(res, providers, log)

	body := map[string]interface{}{
		"model":      "claude-sonnet-4",
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 100,
		"stream":     true,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := &flushableRecorder{httptest.NewRecorder()}
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	output := w.Body.String()
	if !strings.Contains(output, "message_start") {
		t.Errorf("expected message_start in output, got: %s", output)
	}
	if !strings.Contains(output, "Hi") {
		t.Errorf("expected Hi in output, got: %s", output)
	}
}

func TestAnthropicHandlerUnknownModel(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{})
	log := discardLog()
	handler := NewAnthropicHandler(res, map[string]provider.Provider{}, log)

	body := map[string]interface{}{
		"model":      "unknown",
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 100,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAnthropicHandlerNoModel(t *testing.T) {
	res := resolver.New(map[string]resolver.Route{})
	log := discardLog()
	handler := NewAnthropicHandler(res, map[string]provider.Provider{}, log)

	body := map[string]interface{}{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(payload))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
