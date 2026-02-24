package provider

import (
	"context"
	"fmt"

	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/tools"
	"github.com/genesis-ssmp/genesis/cli/internal/logging"
	"github.com/genesis-ssmp/genesis/cli/internal/message"
)

// FallbackProvider wraps multiple providers and falls back on errors.
type FallbackProvider struct {
	providers []Provider
	current   int
}

// NewFallbackProvider creates a provider that falls back through the given list on errors.
func NewFallbackProvider(providers ...Provider) *FallbackProvider {
	return &FallbackProvider{
		providers: providers,
		current:   0,
	}
}

func (f *FallbackProvider) SendMessages(ctx context.Context, messages []message.Message, t []tools.BaseTool) (*ProviderResponse, error) {
	for i := f.current; i < len(f.providers); i++ {
		resp, err := f.providers[i].SendMessages(ctx, messages, t)
		if err == nil {
			return resp, nil
		}
		logging.Warn("Provider failed, trying fallback",
			"provider", f.providers[i].Model().Provider,
			"error", err,
			"fallback_idx", i+1,
		)
	}
	return nil, fmt.Errorf("all providers failed")
}

func (f *FallbackProvider) StreamResponse(ctx context.Context, messages []message.Message, t []tools.BaseTool) <-chan ProviderEvent {
	if f.current < len(f.providers) {
		return f.providers[f.current].StreamResponse(ctx, messages, t)
	}
	ch := make(chan ProviderEvent, 1)
	ch <- ProviderEvent{Type: EventError, Error: fmt.Errorf("no providers available")}
	close(ch)
	return ch
}

func (f *FallbackProvider) Model() models.Model {
	if f.current < len(f.providers) {
		return f.providers[f.current].Model()
	}
	return models.Model{}
}
