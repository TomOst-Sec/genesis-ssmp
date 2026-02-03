package heaven

import "fmt"

// ErrPFBudgetExceeded is returned when a mission exceeds its PF budget.
var ErrPFBudgetExceeded = fmt.Errorf("pf: budget exceeded")

// PFGovernorConfig holds per-mission PF budget limits.
type PFGovernorConfig struct {
	MaxPFCalls    int   // default: 20
	MaxShardBytes int64 // default: 65536 (64KB)
}

// DefaultPFGovernorConfig returns sensible defaults.
func DefaultPFGovernorConfig() PFGovernorConfig {
	return PFGovernorConfig{
		MaxPFCalls:    20,
		MaxShardBytes: 65536,
	}
}

// PFGovernor wraps a PFRouter and enforces per-mission PF budgets.
type PFGovernor struct {
	router *PFRouter
	config PFGovernorConfig
}

// NewPFGovernor creates a PF governor wrapping the given router.
func NewPFGovernor(router *PFRouter, config PFGovernorConfig) *PFGovernor {
	return &PFGovernor{
		router: router,
		config: config,
	}
}

// Handle checks the PF budget and delegates to the underlying router.
func (g *PFGovernor) Handle(req PFRequest) (PFResponse, error) {
	m := g.router.getMetrics(req.Args.MissionID)

	// Pre-check: reject if already over budget
	if int(m.pfCount.Load()) >= g.config.MaxPFCalls {
		return PFResponse{}, fmt.Errorf("%w: pf_count %d >= limit %d",
			ErrPFBudgetExceeded, m.pfCount.Load(), g.config.MaxPFCalls)
	}
	if m.shardBytes.Load() >= g.config.MaxShardBytes {
		return PFResponse{}, fmt.Errorf("%w: shard_bytes %d >= limit %d",
			ErrPFBudgetExceeded, m.shardBytes.Load(), g.config.MaxShardBytes)
	}

	// Delegate to real router
	resp, err := g.router.Handle(req)
	if err != nil {
		return resp, err
	}

	// Annotate response with budget remaining
	resp.Meta.BudgetRemaining = &PFBudgetRemaining{
		PFCallsLeft:    g.config.MaxPFCalls - int(resp.Meta.PFCount),
		ShardBytesLeft: g.config.MaxShardBytes - resp.Meta.ShardBytes,
	}

	return resp, nil
}
