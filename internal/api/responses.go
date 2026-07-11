package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"github.com/hamachi-dev/polyllm-gateway/internal/logger"
	"github.com/hamachi-dev/polyllm-gateway/internal/model"
	"github.com/hamachi-dev/polyllm-gateway/internal/provider"
	"github.com/hamachi-dev/polyllm-gateway/internal/resolver"
	"strings"
	"time"
)

type ResponsesHandler struct {
	resolver  *resolver.ModelResolver
	providers map[string]provider.Provider
	log       *slog.Logger
}

func NewResponsesHandler(resolver *resolver.ModelResolver, providers map[string]provider.Provider, log *slog.Logger) *ResponsesHandler {
	return &ResponsesHandler{
		resolver:  resolver,
		providers: providers,
		log:       log,
	}
}

func (h *ResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reqID := logger.GetRequestID(ctx)
	h.log.Info("responses request", "request_id", reqID, "path", r.URL.Path)

	var body struct {
		Model        string          `json:"model"`
		Instructions string          `json:"instructions"`
		Input        json.RawMessage `json:"input"`
		Stream       bool            `json:"stream"`
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

	messages := make([]model.Message, 0)
	if body.Instructions != "" {
		messages = append(messages, model.Message{Role: "system", Content: body.Instructions})
	}
	inputMessages := convertInputToMessages(body.Input)
	messages = append(messages, inputMessages...)
	if len(messages) == 0 {
		messages = []model.Message{{Role: "user", Content: string(body.Input)}}
	}

	chatReq := &model.ChatRequest{
		Model:    route.Model,
		Messages: messages,
		Stream:   body.Stream,
		API:      route.API,
	}

	resp, err := p.Chat(ctx, chatReq)
	if err != nil {
		h.log.Error("provider chat error", "request_id", reqID, "error", err)
		writeOpenAIError(w, http.StatusInternalServerError, "upstream error")
		return
	}

	if body.Stream {
		h.handleStreaming(ctx, w, reqID, resp, body.Model, route, start)
		return
	}

	respBody := map[string]interface{}{
		"id":      fmt.Sprintf("resp_%s", reqID),
		"object":  "response",
		"model":   body.Model,
		"output": []map[string]interface{}{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]string{
					{
						"type": "output_text",
						"text": resp.Message.Content,
					},
				},
			},
		},
	}

	if resp.Usage != nil {
		respBody["usage"] = map[string]interface{}{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
			"total_tokens":  resp.Usage.TotalTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(respBody)

	h.log.Info("responses completed",
		"request_id", reqID,
		"provider", route.Provider,
		"route", body.Model,
		"latency", time.Since(start),
		"status_code", http.StatusOK,
	)
}

func (h *ResponsesHandler) handleStreaming(ctx context.Context, w http.ResponseWriter, reqID string, resp *model.ChatResponse, clientModel string, route resolver.Route, start time.Time) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	if resp.StreamBody == nil {
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	defer resp.StreamBody.Close()

	responseID := fmt.Sprintf("resp_%s", reqID)
	itemID := fmt.Sprintf("msg_%s", reqID)
	hasSentCreated := false
	hasSentItem := false
	var fullContent strings.Builder

	buf := make([]byte, 65536)
	remainder := ""

	emit := func(data map[string]interface{}) {
		eventType, _ := data["type"].(string)
		writeResponseSSE(w, eventType, toJSON(data))
	}

	for {
		n, err := resp.StreamBody.Read(buf)
		if n > 0 {
			remainder += string(buf[:n])
			for {
				idx := strings.Index(remainder, "\n\n")
				if idx < 0 {
					break
				}
				line := remainder[:idx]
				remainder = remainder[idx+2:]

				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					if hasSentItem {
						emit(map[string]interface{}{
							"type":         "response.output_item.done",
							"output_index": 0,
							"item": map[string]interface{}{
								"type":    "message",
								"id":      itemID,
								"role":    "assistant",
								"status":  "completed",
								"content": []map[string]string{{"type": "output_text", "text": fullContent.String()}},
							},
						})
					}
					emit(map[string]interface{}{
						"type": "response.completed",
						"response": map[string]interface{}{
							"id":      responseID,
							"status":  "completed",
							"model":   clientModel,
							"output":  []map[string]interface{}{{"type": "message", "id": itemID, "role": "assistant", "status": "completed", "content": []map[string]string{{"type": "output_text", "text": fullContent.String()}}}},
						},
					})
					if flusher != nil {
						flusher.Flush()
					}
					return
				}

				var chunk struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
							Role    string `json:"role"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue
				}

				if !hasSentCreated {
					emit(map[string]interface{}{
						"type": "response.created",
						"response": map[string]interface{}{
							"id":     responseID,
							"status": "in_progress",
							"model":  clientModel,
						},
					})
					hasSentCreated = true
				}

				if !hasSentItem && len(chunk.Choices) > 0 {
					emit(map[string]interface{}{
						"type":         "response.output_item.added",
						"output_index": 0,
						"item": map[string]interface{}{
							"type":    "message",
							"id":      itemID,
							"role":    "assistant",
							"status":  "in_progress",
							"content": []interface{}{},
						},
					})
					hasSentItem = true
				}

				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
					fullContent.WriteString(chunk.Choices[0].Delta.Content)
					emit(map[string]interface{}{
						"type":         "response.output_text.delta",
						"item_id":      itemID,
						"output_index": 0,
						"delta":        chunk.Choices[0].Delta.Content,
					})
				}

				if flusher != nil {
					flusher.Flush()
				}
			}
		}
		if err != nil {
			if !hasSentCreated {
				emit(map[string]interface{}{
					"type": "response.created",
					"response": map[string]interface{}{
						"id":     responseID,
						"status": "in_progress",
						"model":  clientModel,
					},
				})
			}
			emit(map[string]interface{}{
				"type": "response.completed",
				"response": map[string]interface{}{
					"id":     responseID,
					"status": "completed",
					"model":  clientModel,
				},
			})
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}

	h.log.Info("responses streaming completed",
		"request_id", reqID,
		"provider", route.Provider,
		"route", clientModel,
		"latency", time.Since(start),
	)
}

func extractTextFromContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		for _, block := range c {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := b["text"].(string); t != "" {
				return t
			}
		}
	}
	return ""
}

func convertInputToMessages(input json.RawMessage) []model.Message {
	var messages []model.Message

	var raw interface{}
	if err := json.Unmarshal(input, &raw); err != nil {
		return nil
	}

	switch v := raw.(type) {
	case string:
		messages = append(messages, model.Message{Role: "user", Content: v})
	case []interface{}:
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			msgType, _ := m["type"].(string)
			if msgType != "message" && msgType != "" {
				continue
			}
			role, _ := m["role"].(string)
			content := extractTextFromContent(m["content"])
			if content != "" || role != "" {
				if role == "developer" {
					role = "system"
				}
				messages = append(messages, model.Message{Role: role, Content: content})
			}
		}
	}

	return messages
}

func writeResponseSSE(w http.ResponseWriter, eventType string, data []byte) {
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	w.Write(buf.Bytes())
}

func toJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
