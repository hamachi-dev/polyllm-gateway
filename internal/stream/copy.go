package stream

import (
	"context"
	"io"
)

type CopyConverter struct{}

func NewCopy() *CopyConverter {
	return &CopyConverter{}
}

func (c *CopyConverter) Convert(ctx context.Context, src io.Reader, dst io.Writer) error {
	_, err := io.Copy(dst, src)
	return err
}
