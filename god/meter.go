package god

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/genesis-ssmp/genesis/internal/lean"
)

// MissionMetrics tracks per-mission metering data.
type MissionMetrics struct {
	MissionID      string  `json:"mission_id"`
	PFCount        int     `json:"pf_count"`
	PFResponseSize int     `json:"pf_response_size_total"` // total bytes across all PFs
	Retries        int     `json:"retries"`
	Rejects        int     `json:"rejects"`        // patch rejections (integration failures)
	Conflicts      int     `json:"conflicts"`       // conflict hunk missions generated
	TestFailures   int     `json:"test_failures"`   // consecutive test failures without progress
	TokensIn       int     `json:"tokens_in"`       // estimated tokens sent
	TokensOut      int     `json:"tokens_out"`      // estimated tokens received
	StartedAt      string  `json:"started_at"`
	ElapsedMS      int64   `json:"elapsed_ms"`
	Turns          int     `json:"turns"`           // number of execution turns
	Status         string  `json:"status"`          // "active", "completed", "thrashing"
	PhaseTransitions int    `json:"phase_transitions,omitempty"`
	ThrashReason     string `json:"thrash_reason,omitempty"`

	// Internal: per-turn PF counts for consecutive-turn detection
	turnPFCounts []int
}

// AvgPFResponseSize returns the average PF response size in bytes.
func (m *MissionMetrics) AvgPFResponseSize() int {
	if m.PFCount == 0 {
		return 0
	}
	return m.PFResponseSize / m.PFCount
}

// MetricsAggregator tracks metrics for all active missions.
type MetricsAggregator struct {
	mu       sync.Mutex
	missions map[string]*MissionMetrics
	heaven   *HeavenClient
}

// NewMetricsAggregator creates a MetricsAggregator backed by the given Heaven client.
func NewMetricsAggregator(heaven *HeavenClient) *MetricsAggregator {
	return &MetricsAggregator{
		missions: make(map[string]*MissionMetrics),
		heaven:   heaven,
	}
}

// StartMission begins tracking metrics for a mission.
func (ma *MetricsAggregator) StartMission(missionID string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	ma.missions[missionID] = &MissionMetrics{
		MissionID: missionID,
		StartedAt: nowFunc().UTC().Format(time.RFC3339),
		Status:    "active",
	}
}

// Get returns the current metrics for a mission, or nil if not tracked.
func (ma *MetricsAggregator) Get(missionID string) *MissionMetrics {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m, ok := ma.missions[missionID]
	if !ok {
		return nil
	}
	// Compute elapsed
	if m.StartedAt != "" && m.Status == "active" {
		if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
			m.ElapsedMS = nowFunc().Sub(t).Milliseconds()
		}
	}
	return m
}

// RecordPF records a page fault request and response size.
func (ma *MetricsAggregator) RecordPF(missionID string, responseBytes int) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.PFCount++
	m.PFResponseSize += responseBytes
}

// RecordProviderUsage records metrics from a provider execution.
func (ma *MetricsAggregator) RecordProviderUsage(usage *ProviderUsage) {
	if usage == nil {
		return
	}
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(usage.MissionID)
	m.Retries += usage.Retries
	m.TokensIn += usage.RequestBytes / 4  // rough estimate: 4 bytes per token
	m.TokensOut += usage.ResponseBytes / 4
}

// RecordReject records a patch rejection (integration failure).
func (ma *MetricsAggregator) RecordReject(missionID string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.Rejects++
}

// RecordConflict records a conflict hunk mission generated.
func (ma *MetricsAggregator) RecordConflict(missionID string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.Conflicts++
}

// RecordTestFailure records a test failure for the mission.
func (ma *MetricsAggregator) RecordTestFailure(missionID string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.TestFailures++
}

// RecordTestPass resets the consecutive test failure counter.
func (ma *MetricsAggregator) RecordTestPass(missionID string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.TestFailures = 0
}

// RecordPhaseTransition records a solo phase transition for a mission.
func (ma *MetricsAggregator) RecordPhaseTransition(missionID, phase string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.PhaseTransitions++
}

// EndTurn records the end of an execution turn, snapshotting the current
// PF count for this turn. Returns the current turn number.
func (ma *MetricsAggregator) EndTurn(missionID string) int {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m := ma.getOrCreate(missionID)
	m.Turns++

	// Compute PFs this turn: total minus sum of previous turns
	prevTotal := 0
	for _, c := range m.turnPFCounts {
		prevTotal += c
	}
	thisTurnPFs := m.PFCount - prevTotal
	m.turnPFCounts = append(m.turnPFCounts, thisTurnPFs)

	return m.Turns
}

// CompleteMission marks a mission as completed and logs the final metrics event.
func (ma *MetricsAggregator) CompleteMission(missionID string) {
	ma.mu.Lock()

	m := ma.getOrCreate(missionID)
	if m.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
			m.ElapsedMS = nowFunc().Sub(t).Milliseconds()
		}
	}
	if m.Status == "active" {
		m.Status = "completed"
	}

	ma.mu.Unlock()

	ma.logMetricsEvent(missionID)
}

// Summary returns a CLI-friendly summary string for a mission's metrics.
func (ma *MetricsAggregator) Summary(missionID string) string {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	m, ok := ma.missions[missionID]
	if !ok {
		return fmt.Sprintf("mission %s: no metrics recorded", missionID)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "mission %s [%s]\n", m.MissionID, m.Status)
	fmt.Fprintf(&b, "  turns:     %d\n", m.Turns)
	fmt.Fprintf(&b, "  pf_count:  %d (avg %d bytes)\n", m.PFCount, m.AvgPFResponseSize())
	fmt.Fprintf(&b, "  retries:   %d\n", m.Retries)
	fmt.Fprintf(&b, "  rejects:   %d\n", m.Rejects)
	fmt.Fprintf(&b, "  conflicts: %d\n", m.Conflicts)
	fmt.Fprintf(&b, "  test_fail: %d\n", m.TestFailures)
	fmt.Fprintf(&b, "  tokens:    %d in / %d out\n", m.TokensIn, m.TokensOut)
	fmt.Fprintf(&b, "  elapsed:   %dms\n", m.ElapsedMS)
	if m.ThrashReason != "" {
		fmt.Fprintf(&b, "  thrash:    %s\n", m.ThrashReason)
	}
	return b.String()
}

// logMetricsEvent writes a metrics_snapshot event to Heaven.
// When GENESIS_LEAN=1, encodes metrics as TSLN for compact storage.
func (ma *MetricsAggregator) logMetricsEvent(missionID string) {
	ma.mu.Lock()
	m, ok := ma.missions[missionID]
	if !ok {
		ma.mu.Unlock()
		return
	}

	// Try lean encoding first
	if lean.Enabled() {
		row := lean.MetricsRow{
			Timestamp:        nowFunc().UTC(),
			MissionID:        m.MissionID,
			Status:           m.Status,
			PFCount:          m.PFCount,
			PFResponseSize:   m.PFResponseSize,
			Retries:          m.Retries,
			Rejects:          m.Rejects,
			Conflicts:        m.Conflicts,
			TestFailures:     m.TestFailures,
			TokensIn:         m.TokensIn,
			TokensOut:        m.TokensOut,
			Turns:            m.Turns,
			ElapsedMS:        m.ElapsedMS,
			PhaseTransitions: m.PhaseTransitions,
		}
		ma.mu.Unlock()

		tslnData, err := lean.EncodeMetricsRow(row)
		if err == nil {
			ma.heaven.AppendEvent(map[string]any{
				"type":       "metrics_snapshot_lean",
				"mission_id": m.MissionID,
				"tsln":       string(tslnData),
			})
			return
		}
		// Fall through to JSON on encode error
	} else {
		ma.mu.Unlock()
	}

	// JSON fallback
	ma.mu.Lock()
	m, ok = ma.missions[missionID]
	if !ok {
		ma.mu.Unlock()
		return
	}
	evt := map[string]any{
		"type":                 "metrics_snapshot",
		"mission_id":          m.MissionID,
		"status":              m.Status,
		"pf_count":            m.PFCount,
		"avg_pf_response_size": m.AvgPFResponseSize(),
		"retries":             m.Retries,
		"rejects":             m.Rejects,
		"conflicts":           m.Conflicts,
		"test_failures":       m.TestFailures,
		"tokens_in":           m.TokensIn,
		"tokens_out":          m.TokensOut,
		"turns":               m.Turns,
		"elapsed_ms":          m.ElapsedMS,
	}
	if m.ThrashReason != "" {
		evt["thrash_reason"] = m.ThrashReason
	}
	ma.mu.Unlock()

	ma.heaven.AppendEvent(evt)
}

// getOrCreate returns the metrics for a mission, creating if absent.
// Caller must hold ma.mu.
func (ma *MetricsAggregator) getOrCreate(missionID string) *MissionMetrics {
	m, ok := ma.missions[missionID]
	if !ok {
		m = &MissionMetrics{
			MissionID: missionID,
			StartedAt: nowFunc().UTC().Format(time.RFC3339),
			Status:    "active",
		}
		ma.missions[missionID] = m
	}
	return m
}
