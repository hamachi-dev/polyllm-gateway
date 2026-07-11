package provider

import (
	"context"
	"github.com/hamachi-dev/polyllm-gateway/internal/model"
)

type Provider interface {
	Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)
}
