package bench

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient is the subset of the Anthropic SDK the bench harness depends on.
// Tests can substitute a fake without hitting the network.
type AnthropicClient interface {
	CreateMessage(ctx context.Context, req anthropic.MessageNewParams) (*anthropic.Message, error)
}

// NewSDKClient wraps the real SDK client behind the AnthropicClient interface.
func NewSDKClient(apiKey string) AnthropicClient {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &sdkClient{inner: c}
}

type sdkClient struct {
	inner anthropic.Client
}

func (s *sdkClient) CreateMessage(ctx context.Context, req anthropic.MessageNewParams) (*anthropic.Message, error) {
	return s.inner.Messages.New(ctx, req)
}
