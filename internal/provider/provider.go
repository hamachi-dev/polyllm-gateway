package provider

import (
	"context"
	"github.com/taizo/polyllm-gateway/internal/model"
)

type Provider interface {
	Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)
}
