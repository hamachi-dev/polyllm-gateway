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

type OpenAIHandler struct {
	resolver *resolver.ModelResolver
	providers map[string]provider.Provider
	log       *slog.Logger
}

func NewOpenAIHandler(resolver *resolver.ModelResolver, providers map[string]provider.Provider, log *slog.Logger) *OpenAIHandler {
	return &OpenAIHandler{
		resolver:  resolver,
		providers: providers,
		log:       log,
	}
}

func (h *OpenAIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reqID := logger.GetRequestID(ctx)
	h.log.Info("openai request", "request_id", reqID, "path", r.URL.Path)

	var body struct {
		Model       string      `json:"model"`
		Messages    []model.Message `json:"messages"`
		Stream      bool        `json:"stream"`
		Temperature *float64    `json:"temperature,omitempty"`
		MaxTokens   *int        `json:"max_tokens,omitempty"`
		Stop        []string    `json:"stop,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required")
		return
	}

	route, ok := h.resolver.Resolve(body.Model)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, fmt.Sprintf("unknown model: %s", body.Model))
		return
	}

	p, ok := h.providers[route.Provider]
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("unknown provider: %s", route.Provider))
		return
	}

	chatReq := &model.ChatRequest{
		Model:       route.Model,
		Messages:    body.Messages,
		Stream:      body.Stream,
		Temperature: body.Temperature,
		MaxTokens:   body.MaxTokens,
		Stop:        body.Stop,
		API:         route.API,
	}

	resp, err := p.Chat(ctx, chatReq)
	if err != nil {
		h.log.Error("provider chat error", "request_id", reqID, "error", err)
		writeOpenAIError(w, http.StatusInternalServerError, "upstream error")
		return
	}

	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)

		conv := stream.NewCopy()
		if err := conv.Convert(ctx, resp.StreamBody, w); err != nil {
			h.log.Error("stream copy error", "request_id", reqID, "error", err)
		}
		resp.StreamBody.Close()
		if flusher != nil {
			flusher.Flush()
		}
		h.log.Info("openai streaming completed",
			"request_id", reqID,
			"provider", route.Provider,
			"route", body.Model,
			"latency", time.Since(start),
		)
		return
	}

	respBody := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%s", reqID),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   body.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    resp.Message.Role,
					"content": resp.Message.Content,
				},
				"finish_reason": resp.FinishReason,
			},
		},
	}

	if resp.Usage != nil {
		respBody["usage"] = resp.Usage
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(respBody)

	h.log.Info("openai completed",
		"request_id", reqID,
		"provider", route.Provider,
		"route", body.Model,
		"latency", time.Since(start),
		"status_code", http.StatusOK,
	)
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"message": message,
			"type":    "api_error",
		},
	})
}

