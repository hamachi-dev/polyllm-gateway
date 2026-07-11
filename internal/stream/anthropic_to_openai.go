package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type anthropicStreamChunk struct {
	Type  string               `json:"type"`
	Index int                  `json:"index,omitempty"`
	Delta *anthropicStreamDelta `json:"delta,omitempty"`
}

type anthropicStreamDelta struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

type AnthropicToOpenAIConverter struct{}

func NewAnthropicToOpenAI() *AnthropicToOpenAIConverter {
	return &AnthropicToOpenAIConverter{}
}

func (c *AnthropicToOpenAIConverter) Convert(ctx context.Context, src io.Reader, dst io.Writer) error {
	scanner := bufio.NewScanner(src)
	flusher, _ := dst.(interface{ Flush() error })
	var hasSentRole bool

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
			continue
		}

		var chunk anthropicStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		switch chunk.Type {
		case "message_start", "content_block_start":
			if !hasSentRole {
				writeSSE(dst, openAIChunkDelta("", "assistant", nil))
				hasSentRole = true
			}

		case "content_block_delta":
			if chunk.Delta != nil && chunk.Delta.Text != "" {
				writeSSE(dst, openAIChunkDelta(chunk.Delta.Text, "", nil))
			}

		case "message_stop":
			reason := "stop"
			writeSSE(dst, openAIChunkDelta("", "", &reason))
			fmt.Fprintf(dst, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}

		if flusher != nil {
			flusher.Flush()
		}
	}

	return scanner.Err()
}

func openAIChunkDelta(content, role string, finishReason *string) []byte {
	delta := map[string]string{}
	if role != "" {
		delta["role"] = role
	}
	if content != "" {
		delta["content"] = content
	}

	choice := map[string]interface{}{
		"index": 0,
		"delta": delta,
	}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	}

	chunk := map[string]interface{}{
		"id":      "chatcmpl",
		"object":  "chat.completion.chunk",
		"model":   "",
		"choices": []interface{}{choice},
	}
	return toJSON(chunk)
}
