package stream

import (
	"context"
	"io"
)

type Converter interface {
	Convert(ctx context.Context, src io.Reader, dst io.Writer) error
}
