package agent

import (
	"context"
	"fmt"

	"github.com/genesis-ssmp/genesis/cli/internal/config"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/cli/internal/message"
	"github.com/genesis-ssmp/genesis/cli/internal/pubsub"
)

// stubAgent implements agent.Service with no LLM provider.
// Used when no provider is configured so the TUI can still boot.
type stubAgent struct {
	*pubsub.Broker[AgentEvent]
}

// NewStubAgent creates an agent that satisfies the Service interface
// but returns errors when asked to do LLM work.
func NewStubAgent() Service {
	return &stubAgent{
		Broker: pubsub.NewBroker[AgentEvent](),
	}
}

func (s *stubAgent) Model() models.Model {
	return models.Model{
		ID:       "offline",
		Name:     "Offline (no provider)",
		Provider: "none",
	}
}

func (s *stubAgent) Run(_ context.Context, _ string, _ string, _ ...message.Attachment) (<-chan AgentEvent, error) {
	return nil, fmt.Errorf("no LLM provider configured — use /connect-provider to add one")
}

func (s *stubAgent) Cancel(_ string) {}

func (s *stubAgent) IsSessionBusy(_ string) bool { return false }

func (s *stubAgent) IsBusy() bool { return false }

func (s *stubAgent) Update(_ config.AgentName, _ models.ModelID) (models.Model, error) {
	return models.Model{}, fmt.Errorf("no LLM provider configured — use /connect-provider to add one")
}

func (s *stubAgent) Summarize(_ context.Context, _ string) error {
	return fmt.Errorf("no LLM provider configured — use /connect-provider to add one")
}
