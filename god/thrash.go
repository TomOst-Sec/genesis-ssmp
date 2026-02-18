package god

import (
	"fmt"
	"sync"
)

// ThrashConfig holds the thresholds for thrash detection.
type ThrashConfig struct {
	PFSoftLimit        int // PF count per turn that triggers soft limit
	PFConsecutiveTurns int // consecutive turns over soft limit to trigger thrash
	MaxRejects         int // patch rejections before thrash
	MaxConflicts       int // conflicts before thrash
	MaxTestFailures    int // consecutive test failures without progress before thrash
}

// DefaultThrashConfig returns reasonable defaults for thrash detection.
func DefaultThrashConfig() ThrashConfig {
	return ThrashConfig{
		PFSoftLimit:        20,
		PFConsecutiveTurns: 2,
		MaxRejects:         2,
		MaxConflicts:       3,
		MaxTestFailures:    3,
	}
}

// ThrashDetector monitors mission metrics and triggers thrash detection.
// Thrash triggers exactly once per mission (latch pattern).
type ThrashDetector struct {
	mu       sync.Mutex
	config   ThrashConfig
	latched  map[string]bool // mission_id -> true if already triggered
	heaven   *HeavenClient
}

// NewThrashDetector creates a ThrashDetector with the given config.
func NewThrashDetector(config ThrashConfig, heaven *HeavenClient) *ThrashDetector {
	return &ThrashDetector{
		config:  config,
		latched: make(map[string]bool),
		heaven:  heaven,
	}
}

// ThrashResult describes the outcome of a thrash check.
type ThrashResult struct {
	Thrashing bool   `json:"thrashing"`
	Reason    string `json:"reason,omitempty"`
}

// Check evaluates whether a mission is thrashing based on its current metrics.
// Returns a ThrashResult. If thrashing is detected for the first time, it
// marks the mission metrics as THRASHING and logs an event.
func (td *ThrashDetector) Check(metrics *MissionMetrics) ThrashResult {
	if metrics == nil {
		return ThrashResult{}
	}

	td.mu.Lock()
	defer td.mu.Unlock()

	// Latch: already triggered for this mission
	if td.latched[metrics.MissionID] {
		return ThrashResult{Thrashing: true, Reason: metrics.ThrashReason}
	}

	reason := td.detect(metrics)
	if reason == "" {
		return ThrashResult{}
	}

	// First trigger — latch
	td.latched[metrics.MissionID] = true
	metrics.Status = "thrashing"
	metrics.ThrashReason = reason

	// Log thrash event
	td.heaven.AppendEvent(map[string]any{
		"type":       "mission_thrashing",
		"mission_id": metrics.MissionID,
		"reason":     reason,
		"pf_count":   metrics.PFCount,
		"rejects":    metrics.Rejects,
		"conflicts":  metrics.Conflicts,
		"test_failures": metrics.TestFailures,
		"turns":      metrics.Turns,
	})

	return ThrashResult{Thrashing: true, Reason: reason}
}

// IsLatched returns true if thrash has already been triggered for this mission.
func (td *ThrashDetector) IsLatched(missionID string) bool {
	td.mu.Lock()
	defer td.mu.Unlock()
	return td.latched[missionID]
}

// detect checks all thrash conditions. Returns the reason or empty string.
func (td *ThrashDetector) detect(m *MissionMetrics) string {
	// Condition 1: PF count exceeds soft limit for N consecutive turns
	if reason := td.checkPFSoftLimit(m); reason != "" {
		return reason
	}

	// Condition 2: Patch rejected MaxRejects times
	if m.Rejects >= td.config.MaxRejects {
		return fmt.Sprintf("patch rejected %d times (limit %d)", m.Rejects, td.config.MaxRejects)
	}

	// Condition 3: Conflicts above threshold
	if m.Conflicts >= td.config.MaxConflicts {
		return fmt.Sprintf("conflicts %d above threshold %d", m.Conflicts, td.config.MaxConflicts)
	}

	// Condition 4: Tests failing repeatedly without progress
	if m.TestFailures >= td.config.MaxTestFailures {
		return fmt.Sprintf("tests failing %d consecutive times without progress (limit %d)", m.TestFailures, td.config.MaxTestFailures)
	}

	return ""
}

// checkPFSoftLimit checks if PF count exceeded the soft limit for consecutive turns.
func (td *ThrashDetector) checkPFSoftLimit(m *MissionMetrics) string {
	needed := td.config.PFConsecutiveTurns
	if needed <= 0 || len(m.turnPFCounts) < needed {
		return ""
	}

	// Check the last N turns
	tail := m.turnPFCounts[len(m.turnPFCounts)-needed:]
	for _, count := range tail {
		if count < td.config.PFSoftLimit {
			return ""
		}
	}

	return fmt.Sprintf("PF count exceeded soft limit (%d) for %d consecutive turns",
		td.config.PFSoftLimit, needed)
}
