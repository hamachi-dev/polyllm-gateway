package logger

import (
	"context"
	"log/slog"
	"os"
	"time"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func New() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

type LogEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	RequestID  string        `json:"request_id,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	Route      string        `json:"route,omitempty"`
	Latency    time.Duration `json:"latency,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Error      string        `json:"error,omitempty"`
	API        string        `json:"api,omitempty"`
}
