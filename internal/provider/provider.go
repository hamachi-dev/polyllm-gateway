package provider

import (
	"context"
	"proxy/internal/model"
)

type Provider interface {
	Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)
}
