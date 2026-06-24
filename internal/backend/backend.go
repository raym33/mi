package backend

import (
	"context"

	"github.com/raym33/mi/internal/protocol"
)

type Runtime interface {
	Name() string
	Chat(ctx context.Context, req protocol.InferRequest, onChunk func(string) error) (protocol.InferDone, error)
	Embed(ctx context.Context, model string, input []string) (protocol.EmbedResult, error)
}
