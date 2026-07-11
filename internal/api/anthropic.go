package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"proxy/internal/logger"
	"proxy/internal/model"
	"proxy/internal/provider"
	"proxy/internal/resolver"
	"proxy/internal/stream"
	"time"
)

type AnthropicHandler struct {
	resolver  *resolver.ModelResolver
	providers map[string]provider.Provider
	log       *slog.Logger
}

func NewAnthropicHandler(resolver *resolver.ModelResolver, providers map[string]provider.Provider, log *slog.Logger) *AnthropicHandler {
	return &AnthropicHandler{
		resolver:  resolver,
		providers: providers,
		log:       log,
	}
}

type anthropicRequest struct {
	Model       string          `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	StopSequences []string      `json:"stop_sequences,omitempty"`
	System      string          `json:"system,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Model   string            `json:"model"`
	Content []anthropicContent `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage   *anthropicUsage   `json:"usage,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (h *AnthropicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reqID := logger.GetRequestID(ctx)
	h.log.Info("anthropic request", "request_id", reqID, "path", r.URL.Path)

	var body anthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "model is required")
		return
	}

	route, ok := h.resolver.Resolve(body.Model)
	if !ok {
		writeAnthropicError(w, http.StatusNotFound, fmt.Sprintf("unknown model: %s", body.Model))
		return
	}

	p, ok := h.providers[route.Provider]
	if !ok {
		writeAnthropicError(w, http.StatusInternalServerError, fmt.Sprintf("unknown provider: %s", route.Provider))
		return
	}

	messages := make([]model.Message, len(body.Messages))
	for i, m := range body.Messages {
		messages[i] = model.Message{Role: m.Role, Content: m.Content}
	}

	var maxTokens *int
	if body.MaxTokens > 0 {
		maxTokens = &body.MaxTokens
	}

	chatReq := &model.ChatRequest{
		Model:       route.Model,
		Messages:    messages,
		Stream:      body.Stream,
		Temperature: body.Temperature,
		MaxTokens:   maxTokens,
		Stop:        body.StopSequences,
	}

	resp, err := p.Chat(ctx, chatReq)
	if err != nil {
		h.log.Error("provider chat error", "request_id", reqID, "error", err)
		writeAnthropicError(w, http.StatusInternalServerError, "upstream error")
		return
	}

	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)

		conv := stream.NewOpenAItoAnthropic(body.Model)
		if err := conv.Convert(ctx, resp.StreamBody, w); err != nil {
			h.log.Error("stream convert error", "request_id", reqID, "error", err)
		}
		resp.StreamBody.Close()
		if flusher != nil {
			flusher.Flush()
		}
		h.log.Info("anthropic streaming completed",
			"request_id", reqID,
			"provider", route.Provider,
			"route", body.Model,
			"latency", time.Since(start),
		)
		return
	}

	respBody := anthropicResponse{
		ID:    fmt.Sprintf("msg_%s", reqID),
		Type:  "message",
		Role:  "assistant",
		Model: body.Model,
		Content: []anthropicContent{
			{Type: "text", Text: resp.Message.Content},
		},
		StopReason: resp.FinishReason,
	}

	if resp.Usage != nil {
		respBody.Usage = &anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}

	if respBody.StopReason == "" {
		respBody.StopReason = "end_turn"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(respBody)

	h.log.Info("anthropic completed",
		"request_id", reqID,
		"provider", route.Provider,
		"route", body.Model,
		"latency", time.Since(start),
		"status_code", http.StatusOK,
	)
}

func writeAnthropicError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	})
}
