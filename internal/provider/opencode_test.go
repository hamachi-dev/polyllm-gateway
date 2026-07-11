package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxy/internal/model"
	"strings"
	"testing"
)

func TestOpenCodeNonStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content type")
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		if body["model"] != "qwen3.7-plus" {
			t.Errorf("expected qwen3.7-plus, got %v", body["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-id",
			"object":  "chat.completion",
			"model":   "qwen3.7-plus",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "Hello, world!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		})
	}))
	defer server.Close()

	p := NewOpenCode(OpenCodeConfig{
		Endpoint: server.URL,
		APIKey:   "test-key",
	})

	token := 100
	resp, err := p.Chat(context.Background(), &model.ChatRequest{
		Model: "qwen3.7-plus",
		Messages: []model.Message{
			{Role: "user", Content: "hi"},
		},
		Stream:    false,
		MaxTokens: &token,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Message.Role != "assistant" {
		t.Errorf("expected assistant, got %s", resp.Message.Role)
	}
	if resp.Message.Content != "Hello, world!" {
		t.Errorf("expected Hello, world!, got %s", resp.Message.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected stop, got %s", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected 30, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenCodeStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}

		for _, c := range chunks {
			w.Write([]byte(c + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewOpenCode(OpenCodeConfig{
		Endpoint: server.URL,
		APIKey:   "test-key",
	})

	resp, err := p.Chat(context.Background(), &model.ChatRequest{
		Model:    "qwen3.7-plus",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.StreamBody == nil {
		t.Fatal("expected StreamBody for streaming response")
	}
	defer resp.StreamBody.Close()

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.StreamBody.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	output := sb.String()
	if !strings.Contains(output, "Hello") {
		t.Errorf("expected Hello in stream output, got: %s", output)
	}
}

func TestOpenCodeUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	p := NewOpenCode(OpenCodeConfig{
		Endpoint: server.URL,
		APIKey:   "bad-key",
	})

	_, err := p.Chat(context.Background(), &model.ChatRequest{
		Model:    "test",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}

func TestOpenCodeContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	p := NewOpenCode(OpenCodeConfig{
		Endpoint: server.URL,
		APIKey:   "test-key",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Chat(ctx, &model.ChatRequest{
		Model:    "test",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
