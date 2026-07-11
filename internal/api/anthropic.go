package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"github.com/taizo/polyllm-gateway/internal/logger"
	"github.com/taizo/polyllm-gateway/internal/model"
	"github.com/taizo/polyllm-gateway/internal/provider"
	"github.com/taizo/polyllm-gateway/internal/resolver"
	"github.com/taizo/polyllm-gateway/internal/stream"
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

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	System        json.RawMessage    `json:"system,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func extractMessageContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

type anthropicContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Model      string             `json:"model"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicUsage    `json:"usage,omitempty"`
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
		h.log.Error("anthropic decode error", "request_id", reqID, "error", err)
		writeAnthropicError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sysLen := len(body.System)
	h.log.Info("anthropic debug", "request_id", reqID, "system_len", sysLen, "msg_count", len(body.Messages), "model", body.Model, "max_tokens", body.MaxTokens, "stream", body.Stream, "tools", len(body.Tools))

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

	messages := make([]model.Message, 0)
	systemContent := extractMessageContent(body.System)
	if systemContent != "" {
		messages = append(messages, model.Message{Role: "system", Content: systemContent})
	}
	for _, m := range body.Messages {
		messages = append(messages, model.Message{Role: m.Role, Content: extractMessageContent(m.Content)})
	}

	var maxTokens *int
	if body.MaxTokens > 0 {
		maxTokens = &body.MaxTokens
	}

	tools := make([]model.ToolDef, len(body.Tools))
	for i, t := range body.Tools {
		tools[i] = model.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		}
	}
	toolChoice := ""
	if len(body.ToolChoice) > 0 {
		toolChoice = string(body.ToolChoice)
	}

	chatReq := &model.ChatRequest{
		Model:       route.Model,
		Messages:    messages,
		Stream:      body.Stream,
		Temperature: body.Temperature,
		MaxTokens:   maxTokens,
		Stop:        body.StopSequences,
		API:         route.API,
		Tools:       tools,
		ToolChoice:  toolChoice,
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

	var contents []anthropicContent
	if resp.Message.Content != "" {
		contents = append(contents, anthropicContent{Type: "text", Text: resp.Message.Content})
	}
	for _, tc := range resp.Message.ToolCalls {
		input := json.RawMessage(tc.Function.Arguments)
		if input == nil {
			input = json.RawMessage("{}")
		}
		contents = append(contents, anthropicContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	stopReason := resp.FinishReason
	if stopReason == "tool_calls" {
		stopReason = "tool_use"
	}
	if stopReason == "stop" {
		stopReason = "end_turn"
	}
	if stopReason == "" {
		stopReason = "end_turn"
	}

	respBody := anthropicResponse{
		ID:         fmt.Sprintf("msg_%s", reqID),
		Type:       "message",
		Role:       "assistant",
		Model:      body.Model,
		Content:    contents,
		StopReason: stopReason,
	}

	if resp.Usage != nil {
		respBody.Usage = &anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
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
