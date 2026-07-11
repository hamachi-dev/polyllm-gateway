package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type OpenAIChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
}

type OpenAIChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role    string `json:"role,omitempty"`
		Content string `json:"content,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type AnthropicEvent struct {
	Type string          `json:"type"`
	raw  json.RawMessage `json:"-"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AnthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

type OpenAItoAnthropicConverter struct {
	model string
}

func NewOpenAItoAnthropic(model string) *OpenAItoAnthropicConverter {
	return &OpenAItoAnthropicConverter{model: model}
}

func (c *OpenAItoAnthropicConverter) Convert(ctx context.Context, src io.Reader, dst io.Writer) error {
	scanner := bufio.NewScanner(src)
	flusher, ok := dst.(interface{ Flush() error })
	if !ok {
		return fmt.Errorf("dst does not support Flush")
	}

	var (
		hasSentStart       bool
		hasSentContentStart bool
		hasFinished        bool
	)

	for scanner.Scan() {
		line := scanner.Text()

		if err := ctx.Err(); err != nil {
			return err
		}

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			if !hasFinished {
				if hasSentContentStart {
					writeSSE(dst, toJSON(AnthropicContentBlock{Type: "content_block_stop"}))
				}
				writeSSE(dst, toJSON(map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]string{
						"stop_reason":   "end_turn",
						"stop_sequence": "",
					},
				}))
				writeSSE(dst, toJSON(map[string]interface{}{
					"type": "message_stop",
				}))
				hasFinished = true
			}
			flusher.Flush()
			continue
		}

		var chunk OpenAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("parse chunk: %w", err)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if !hasSentStart {
			writeSSE(dst, toJSON(map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":      chunk.ID,
					"type":    "message",
					"role":    "assistant",
					"model":   c.model,
					"content": []interface{}{},
				},
			}))
			hasSentStart = true
		}

		if choice.Delta.Role == "assistant" && !hasSentContentStart {
			writeSSE(dst, toJSON(map[string]interface{}{
				"type": "content_block_start",
				"index": 0,
				"content_block": AnthropicContentBlock{
					Type: "text",
					Text: "",
				},
			}))
			hasSentContentStart = true
		}

		if choice.Delta.Content != "" && hasSentContentStart {
			writeSSE(dst, toJSON(map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": AnthropicDelta{
					Type: "text_delta",
					Text: choice.Delta.Content,
				},
			}))
		}

		if !hasFinished && choice.FinishReason != nil && *choice.FinishReason != "" {
			if hasSentContentStart {
				writeSSE(dst, toJSON(map[string]interface{}{
					"type":  "content_block_stop",
					"index": 0,
				}))
			}
			writeSSE(dst, toJSON(map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]string{
					"stop_reason":   *choice.FinishReason,
					"stop_sequence": "",
				},
			}))
			writeSSE(dst, toJSON(map[string]interface{}{
				"type": "message_stop",
			}))
			hasFinished = true
		}

		flusher.Flush()
	}

	return scanner.Err()
}

func writeSSE(w io.Writer, data []byte) {
	fmt.Fprintf(w, "data: %s\n\n", string(data))
}

func toJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
