package stream

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
)

type mockFlusher struct {
	*bytes.Buffer
}

func (m *mockFlusher) Flush() error {
	return nil
}

func readSSELines(t *testing.T, data string) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
	return lines
}

func TestOpenAItoAnthropic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantMsgs []string // message types we expect
	}{
		{
			name: "basic streaming",
			input: `data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`,
			wantMsgs: []string{
				"message_start",
				"content_block_start",
				"content_block_delta",
				"content_block_stop",
				"message_delta",
				"message_stop",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := NewOpenAItoAnthropic("m")
			var buf bytes.Buffer
			mockBuf := &mockFlusher{Buffer: &buf}

			err := conv.Convert(context.Background(),
				strings.NewReader(tt.input),
				mockBuf,
			)
			if err != nil {
				t.Fatalf("Convert failed: %v", err)
			}

			msgs := readSSELines(t, buf.String())
			var types []string
			for _, m := range msgs {
				if strings.Contains(m, `"type":`) {
					for _, want := range tt.wantMsgs {
						if strings.Contains(m, `"type":"`+want+`"`) {
							types = append(types, want)
						}
					}
				}
			}

			if len(types) != len(tt.wantMsgs) {
				t.Errorf("got %d events, want %d\ngot: %v", len(types), len(tt.wantMsgs), types)
			}

			if !strings.Contains(buf.String(), "Hello") {
				t.Error("expected Hello in output")
			}
		})
	}
}

func TestOpenAItoAnthropicContextCancel(t *testing.T) {
	conv := NewOpenAItoAnthropic("m")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	mockBuf := &mockFlusher{Buffer: &buf}

	err := conv.Convert(ctx,
		strings.NewReader(`data: {"id":"x","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`),
		mockBuf,
	)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
