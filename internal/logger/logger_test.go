package logger

import (
	"context"
	"testing"
)

func TestRequestID(t *testing.T) {
	ctx := context.Background()

	id := GetRequestID(ctx)
	if id != "" {
		t.Errorf("expected empty, got %s", id)
	}

	ctx = WithRequestID(ctx, "test-id-123")
	id = GetRequestID(ctx)
	if id != "test-id-123" {
		t.Errorf("expected test-id-123, got %s", id)
	}
}

func TestNewLogger(t *testing.T) {
	log := New()
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}
