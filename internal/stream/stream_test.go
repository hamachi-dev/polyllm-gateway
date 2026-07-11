package stream

import (
	"context"
	"strings"
	"testing"
)

func TestCopy(t *testing.T) {
	conv := NewCopy()
	var buf strings.Builder

	err := conv.Convert(context.Background(),
		strings.NewReader("test data"),
		&buf,
	)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if buf.String() != "test data" {
		t.Errorf("expected 'test data', got '%s'", buf.String())
	}
}
