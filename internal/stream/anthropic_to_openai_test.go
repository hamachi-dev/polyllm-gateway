package stream

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type nopFlusher struct {
	*bytes.Buffer
}

func (n *nopFlusher) Flush() error {
	return nil
}

func TestAnthropicToOpenAI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantText string
	}{
		{
			name: "basic streaming",
			input: `data: {"type":"message_start","message":{"id":"msg_1"}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello Anthropic"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`,
			wantText: "Hello Anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := NewAnthropicToOpenAI()
			var buf bytes.Buffer
			mock := &nopFlusher{Buffer: &buf}

			err := conv.Convert(context.Background(), strings.NewReader(tt.input), mock)
			if err != nil {
				t.Fatalf("Convert failed: %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, tt.wantText) {
				t.Errorf("expected '%s' in output, got: %s", tt.wantText, output)
			}
			if !strings.Contains(output, "[DONE]") {
				t.Errorf("expected [DONE] in output, got: %s", output)
			}
			if !strings.Contains(output, `data:`) {
				t.Errorf("expected SSE data: prefix, got: %s", output)
			}
		})
	}
}

func TestAnthropicToOpenAIEmptyDelta(t *testing.T) {
	conv := NewAnthropicToOpenAI()
	var buf bytes.Buffer
	mock := &nopFlusher{Buffer: &buf}

	input := `data: {"type":"message_start"}

data: {"type":"content_block_start","index":0}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

data: {"type":"message_stop"}
`

	err := conv.Convert(context.Background(), strings.NewReader(input), mock)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Hi") {
		t.Errorf("expected Hi in output, got: %s", output)
	}
}
