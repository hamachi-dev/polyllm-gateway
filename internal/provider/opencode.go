package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"proxy/internal/model"
)

type OpenCodeConfig struct {
	Endpoint string
	APIKey   string
}

type OpenCodeProvider struct {
	config OpenCodeConfig
	client *http.Client
}

func NewOpenCode(cfg OpenCodeConfig) *OpenCodeProvider {
	return &OpenCodeProvider{
		config: cfg,
		client: &http.Client{},
	}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
}

type openAIChoice struct {
	Index        int            `json:"index"`
	Message      openAIMessage  `json:"message,omitempty"`
	Delta        *openAIMessage `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChatResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage  `json:"usage,omitempty"`
}

func buildOpenAIRequest(req *model.ChatRequest, endpoint, apiKey string) (*http.Request, error) {
	body := openAIChatRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, openAIMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	return httpReq, nil
}

func (p *OpenCodeProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	httpReq, err := buildOpenAIRequest(req, p.config.Endpoint, p.config.APIKey)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq = httpReq.WithContext(ctx)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	if req.Stream {
		return &model.ChatResponse{
			Model:      req.Model,
			StreamBody: resp.Body,
		}, nil
	}
	defer resp.Body.Close()

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	cr := &model.ChatResponse{
		Model: oaiResp.Model,
	}
	if len(oaiResp.Choices) > 0 {
		cr.Message = model.Message{
			Role:    oaiResp.Choices[0].Message.Role,
			Content: oaiResp.Choices[0].Message.Content,
		}
		if oaiResp.Choices[0].FinishReason != nil {
			cr.FinishReason = *oaiResp.Choices[0].FinishReason
		}
	}
	if oaiResp.Usage != nil {
		cr.Usage = &model.Usage{
			PromptTokens:     oaiResp.Usage.PromptTokens,
			CompletionTokens: oaiResp.Usage.CompletionTokens,
			TotalTokens:      oaiResp.Usage.TotalTokens,
		}
	}

	return cr, nil
}
