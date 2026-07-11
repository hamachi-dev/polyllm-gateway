package model

import "io"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatRequest struct {
	Model       string
	Messages    []Message
	Stream      bool
	Temperature *float64
	MaxTokens   *int
	Stop        []string
}

type ChatResponse struct {
	Model        string
	Message      Message
	FinishReason string
	Usage        *Usage
	StreamBody   io.ReadCloser
}
