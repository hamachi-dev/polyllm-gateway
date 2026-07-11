package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"github.com/hamachi-dev/polyllm-gateway/internal/model"
	"github.com/hamachi-dev/polyllm-gateway/internal/stream"
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
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content"`
	ToolCalls        []openAIToolCall  `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string       `json:"type"`
	Function openAIFunc   `json:"function"`
}

type openAIFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"`
}

type openAIToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function openAIFunc   `json:"function"`
}

type openAIChoice struct {
	Index        int              `json:"index"`
	Message      openAIMessage    `json:"message,omitempty"`
	Delta        *openAIMessage   `json:"delta,omitempty"`
	FinishReason *string          `json:"finish_reason,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
}

type anthropicContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking,omitempty"`
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

func (p *OpenCodeProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	switch req.API {
	case "anthropic":
		return p.chatAnthropic(ctx, req)
	default:
		return p.chatOpenAI(ctx, req)
	}
}

func (p *OpenCodeProvider) chatOpenAI(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	body := openAIChatRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		ToolChoice:  req.ToolChoice,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, openAIMessage{Role: m.Role, Content: m.Content})
	}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, openAITool{
			Type: "function",
			Function: openAIFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	return p.doRoundTrip(ctx, req, httpReq)
}

func (p *OpenCodeProvider) chatAnthropic(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	maxTokens := 1024
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	body := anthropicRequest{
		Model:         req.Model,
		Stream:        req.Stream,
		MaxTokens:     maxTokens,
		Temperature:   req.Temperature,
		StopSequences: req.Stop,
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	payload, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint+"/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	return p.doRoundTrip(ctx, req, httpReq)
}

func (p *OpenCodeProvider) doRoundTrip(ctx context.Context, req *model.ChatRequest, httpReq *http.Request) (*model.ChatResponse, error) {
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
		return p.handleStreaming(req, resp)
	}
	defer resp.Body.Close()

	return p.parseResponse(req, resp)
}

func (p *OpenCodeProvider) handleStreaming(req *model.ChatRequest, resp *http.Response) (*model.ChatResponse, error) {
	if req.API == "anthropic" {
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read upstream body: %w", err)
		}
		var buf bytes.Buffer
		conv := stream.NewAnthropicToOpenAI()
		if err := conv.Convert(context.Background(), bytes.NewReader(raw), &buf); err != nil {
			return nil, fmt.Errorf("convert stream: %w", err)
		}
		return &model.ChatResponse{
			Model:      req.Model,
			StreamBody: io.NopCloser(bytes.NewReader(buf.Bytes())),
		}, nil
	}
	return &model.ChatResponse{Model: req.Model, StreamBody: resp.Body}, nil
}

func (p *OpenCodeProvider) parseResponse(req *model.ChatRequest, resp *http.Response) (*model.ChatResponse, error) {
	if req.API == "anthropic" {
		return p.parseAnthropicResponse(resp)
	}
	return p.parseOpenAIResponse(resp)
}

func (p *OpenCodeProvider) parseOpenAIResponse(resp *http.Response) (*model.ChatResponse, error) {
	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	cr := &model.ChatResponse{Model: oaiResp.Model}
	if len(oaiResp.Choices) > 0 {
		msg := oaiResp.Choices[0].Message
		content := msg.Content
		if content == "" {
			content = msg.ReasoningContent
		}
		cr.Message = model.Message{Role: msg.Role, Content: content}
		for _, tc := range msg.ToolCalls {
			cr.Message.ToolCalls = append(cr.Message.ToolCalls, model.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: model.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
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

func (p *OpenCodeProvider) parseAnthropicResponse(resp *http.Response) (*model.ChatResponse, error) {
	var anthResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	cr := &model.ChatResponse{Model: anthResp.Model}
	if len(anthResp.Content) > 0 {
		cr.Message = model.Message{Role: "assistant", Content: anthResp.Content[0].Text}
	}
	cr.FinishReason = anthResp.StopReason
	if anthResp.Usage != nil {
		cr.Usage = &model.Usage{
			PromptTokens:     anthResp.Usage.InputTokens,
			CompletionTokens: anthResp.Usage.OutputTokens,
			TotalTokens:      anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens,
		}
	}
	return cr, nil
}
