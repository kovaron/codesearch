package embedder

import "context"

// Embedder converts text into a float32 vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
