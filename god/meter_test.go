package god

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock Heaven for metering tests
// ---------------------------------------------------------------------------

func startMeterHeaven(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()

	var mu sync.Mutex
	events := &[]map[string]any{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt map[string]any
		json.Unmarshal(body, &evt)
		mu.Lock()
		*events = append(*events, evt)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"offset": len(*events)})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, events
}

// ---------------------------------------------------------------------------
// MetricsAggregator tests
// ---------------------------------------------------------------------------

func TestMetricsStartMission(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	m := ma.Get("m1")
	if m == nil {
		t.Fatal("expected metrics for m1")
	}
	if m.Status != "active" {
		t.Errorf("Status = %q, want active", m.Status)
	}
	if m.MissionID != "m1" {
		t.Errorf("MissionID = %q", m.MissionID)
	}
	if m.StartedAt == "" {
		t.Error("StartedAt should not be empty")
	}
}

func TestMetricsGetMissing(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	if m := ma.Get("nonexistent"); m != nil {
		t.Error("expected nil for untracked mission")
	}
}

func TestMetricsRecordPF(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordPF("m1", 1000)
	ma.RecordPF("m1", 2000)
	ma.RecordPF("m1", 3000)

	m := ma.Get("m1")
	if m.PFCount != 3 {
		t.Errorf("PFCount = %d, want 3", m.PFCount)
	}
	if m.PFResponseSize != 6000 {
		t.Errorf("PFResponseSize = %d, want 6000", m.PFResponseSize)
	}
	if m.AvgPFResponseSize() != 2000 {
		t.Errorf("AvgPFResponseSize = %d, want 2000", m.AvgPFResponseSize())
	}
}

func TestMetricsAvgPFResponseSizeZero(t *testing.T) {
	m := &MissionMetrics{}
	if m.AvgPFResponseSize() != 0 {
		t.Errorf("AvgPFResponseSize = %d for zero PFs", m.AvgPFResponseSize())
	}
}

func TestMetricsRecordProviderUsage(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordProviderUsage(&ProviderUsage{
		MissionID:     "m1",
		RequestBytes:  4000,
		ResponseBytes: 2000,
		Retries:       1,
	})

	m := ma.Get("m1")
	if m.Retries != 1 {
		t.Errorf("Retries = %d", m.Retries)
	}
	if m.TokensIn != 1000 { // 4000/4
		t.Errorf("TokensIn = %d, want 1000", m.TokensIn)
	}
	if m.TokensOut != 500 { // 2000/4
		t.Errorf("TokensOut = %d, want 500", m.TokensOut)
	}
}

func TestMetricsRecordProviderUsageNil(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	// Should not panic
	ma.RecordProviderUsage(nil)
}

func TestMetricsRecordReject(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordReject("m1")
	ma.RecordReject("m1")

	m := ma.Get("m1")
	if m.Rejects != 2 {
		t.Errorf("Rejects = %d, want 2", m.Rejects)
	}
}

func TestMetricsRecordConflict(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordConflict("m1")

	m := ma.Get("m1")
	if m.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", m.Conflicts)
	}
}

func TestMetricsRecordTestFailure(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordTestFailure("m1")
	ma.RecordTestFailure("m1")

	m := ma.Get("m1")
	if m.TestFailures != 2 {
		t.Errorf("TestFailures = %d, want 2", m.TestFailures)
	}

	// Pass resets consecutive count
	ma.RecordTestPass("m1")
	m = ma.Get("m1")
	if m.TestFailures != 0 {
		t.Errorf("TestFailures after pass = %d, want 0", m.TestFailures)
	}
}

func TestMetricsEndTurn(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordPF("m1", 100)
	ma.RecordPF("m1", 100)
	turn := ma.EndTurn("m1")
	if turn != 1 {
		t.Errorf("turn = %d, want 1", turn)
	}

	ma.RecordPF("m1", 100)
	turn = ma.EndTurn("m1")
	if turn != 2 {
		t.Errorf("turn = %d, want 2", turn)
	}

	m := ma.Get("m1")
	if m.Turns != 2 {
		t.Errorf("Turns = %d, want 2", m.Turns)
	}
}

func TestMetricsCompleteMission(t *testing.T) {
	ts, events := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordPF("m1", 500)
	ma.CompleteMission("m1")

	m := ma.Get("m1")
	if m.Status != "completed" {
		t.Errorf("Status = %q, want completed", m.Status)
	}
	if m.ElapsedMS < 0 {
		t.Errorf("ElapsedMS = %d, want >= 0", m.ElapsedMS)
	}

	// Check that a metrics_snapshot event was logged
	found := false
	for _, e := range *events {
		if e["type"] == "metrics_snapshot" && e["mission_id"] == "m1" {
			found = true
			if e["status"] != "completed" {
				t.Errorf("event status = %v", e["status"])
			}
		}
	}
	if !found {
		t.Error("expected metrics_snapshot event")
	}
}

func TestMetricsAutoCreate(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	// Recording without StartMission should auto-create
	ma.RecordPF("m2", 100)
	m := ma.Get("m2")
	if m == nil {
		t.Fatal("expected auto-created metrics")
	}
	if m.PFCount != 1 {
		t.Errorf("PFCount = %d", m.PFCount)
	}
}

func TestMetricsSummary(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	ma.StartMission("m1")
	ma.RecordPF("m1", 1000)
	ma.RecordReject("m1")
	ma.EndTurn("m1")

	s := ma.Summary("m1")
	if !strings.Contains(s, "m1") {
		t.Error("summary should contain mission ID")
	}
	if !strings.Contains(s, "pf_count:  1") {
		t.Errorf("summary missing pf_count: %s", s)
	}
	if !strings.Contains(s, "rejects:   1") {
		t.Errorf("summary missing rejects: %s", s)
	}
	if !strings.Contains(s, "active") {
		t.Errorf("summary missing status: %s", s)
	}
}

func TestMetricsSummaryUnknown(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	s := ma.Summary("nonexistent")
	if !strings.Contains(s, "no metrics recorded") {
		t.Errorf("summary = %q", s)
	}
}

// ---------------------------------------------------------------------------
// ThrashDetector tests
// ---------------------------------------------------------------------------

func TestThrashNoTrigger(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	td := NewThrashDetector(DefaultThrashConfig(), client)

	m := &MissionMetrics{
		MissionID: "m1",
		Status:    "active",
	}

	result := td.Check(m)
	if result.Thrashing {
		t.Error("should not thrash with zero metrics")
	}
}

func TestThrashNilMetrics(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	td := NewThrashDetector(DefaultThrashConfig(), client)

	result := td.Check(nil)
	if result.Thrashing {
		t.Error("should not thrash with nil metrics")
	}
}

func TestThrashPFSoftLimit(t *testing.T) {
	ts, events := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := ThrashConfig{
		PFSoftLimit:        5,
		PFConsecutiveTurns: 2,
		MaxRejects:         100,
		MaxConflicts:       100,
		MaxTestFailures:    100,
	}
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID:    "m1",
		Status:       "active",
		turnPFCounts: []int{6, 7}, // 2 consecutive turns over limit 5
	}

	result := td.Check(m)
	if !result.Thrashing {
		t.Fatal("should trigger thrash on PF soft limit")
	}
	if !strings.Contains(result.Reason, "PF count exceeded") {
		t.Errorf("reason = %q", result.Reason)
	}
	if m.Status != "thrashing" {
		t.Errorf("status = %q, want thrashing", m.Status)
	}

	// Check event logged
	found := false
	for _, e := range *events {
		if e["type"] == "mission_thrashing" {
			found = true
		}
	}
	if !found {
		t.Error("expected mission_thrashing event")
	}
}

func TestThrashPFSoftLimitNotConsecutive(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := ThrashConfig{
		PFSoftLimit:        5,
		PFConsecutiveTurns: 2,
		MaxRejects:         100,
		MaxConflicts:       100,
		MaxTestFailures:    100,
	}
	td := NewThrashDetector(config, client)

	// Only 1 turn over, 1 under — should not trigger
	m := &MissionMetrics{
		MissionID:    "m1",
		Status:       "active",
		turnPFCounts: []int{6, 3},
	}

	result := td.Check(m)
	if result.Thrashing {
		t.Error("should not thrash when only 1 of 2 turns exceeds limit")
	}
}

func TestThrashPatchRejected(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxRejects = 2
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID: "m1",
		Status:    "active",
		Rejects:   2,
	}

	result := td.Check(m)
	if !result.Thrashing {
		t.Fatal("should trigger thrash on 2 rejects")
	}
	if !strings.Contains(result.Reason, "rejected") {
		t.Errorf("reason = %q", result.Reason)
	}
}

func TestThrashConflicts(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxConflicts = 3
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID: "m1",
		Status:    "active",
		Conflicts: 3,
	}

	result := td.Check(m)
	if !result.Thrashing {
		t.Fatal("should trigger thrash on 3 conflicts")
	}
	if !strings.Contains(result.Reason, "conflicts") {
		t.Errorf("reason = %q", result.Reason)
	}
}

func TestThrashTestFailures(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxTestFailures = 3
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID:    "m1",
		Status:       "active",
		TestFailures: 3,
	}

	result := td.Check(m)
	if !result.Thrashing {
		t.Fatal("should trigger thrash on 3 test failures")
	}
	if !strings.Contains(result.Reason, "tests failing") {
		t.Errorf("reason = %q", result.Reason)
	}
}

func TestThrashTriggersOncePerMission(t *testing.T) {
	ts, events := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxRejects = 1
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID: "m1",
		Status:    "active",
		Rejects:   1,
	}

	// First check — triggers
	result1 := td.Check(m)
	if !result1.Thrashing {
		t.Fatal("first check should trigger")
	}

	// Second check — already latched, still returns thrashing
	m.Rejects = 5 // even worse, but shouldn't re-trigger event
	result2 := td.Check(m)
	if !result2.Thrashing {
		t.Fatal("second check should still report thrashing")
	}

	// Count thrash events — should be exactly 1
	thrashEvents := 0
	for _, e := range *events {
		if e["type"] == "mission_thrashing" {
			thrashEvents++
		}
	}
	if thrashEvents != 1 {
		t.Errorf("thrash events = %d, want exactly 1", thrashEvents)
	}
}

func TestThrashIsLatched(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxRejects = 1
	td := NewThrashDetector(config, client)

	if td.IsLatched("m1") {
		t.Error("should not be latched before check")
	}

	m := &MissionMetrics{MissionID: "m1", Status: "active", Rejects: 1}
	td.Check(m)

	if !td.IsLatched("m1") {
		t.Error("should be latched after trigger")
	}
}

func TestThrashDifferentMissions(t *testing.T) {
	ts, events := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	config.MaxRejects = 1
	td := NewThrashDetector(config, client)

	m1 := &MissionMetrics{MissionID: "m1", Status: "active", Rejects: 1}
	m2 := &MissionMetrics{MissionID: "m2", Status: "active", Rejects: 1}

	td.Check(m1)
	td.Check(m2)

	// Each mission should trigger independently
	thrashEvents := 0
	for _, e := range *events {
		if e["type"] == "mission_thrashing" {
			thrashEvents++
		}
	}
	if thrashEvents != 2 {
		t.Errorf("thrash events = %d, want 2 (one per mission)", thrashEvents)
	}
}

func TestThrashBelowAllThresholds(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)

	config := DefaultThrashConfig()
	td := NewThrashDetector(config, client)

	m := &MissionMetrics{
		MissionID:    "m1",
		Status:       "active",
		Rejects:      1,  // below MaxRejects=2
		Conflicts:    2,  // below MaxConflicts=3
		TestFailures: 2,  // below MaxTestFailures=3
		turnPFCounts: []int{10, 3}, // not consecutive
	}

	result := td.Check(m)
	if result.Thrashing {
		t.Errorf("should not thrash below all thresholds, reason: %s", result.Reason)
	}
}

// ---------------------------------------------------------------------------
// Integration: Aggregator + ThrashDetector
// ---------------------------------------------------------------------------

func TestMeterThrashIntegration(t *testing.T) {
	ts, events := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	config := DefaultThrashConfig()
	config.MaxRejects = 2
	td := NewThrashDetector(config, client)

	ma.StartMission("m1")
	ma.RecordReject("m1")

	// Not thrashing yet
	m := ma.Get("m1")
	result := td.Check(m)
	if result.Thrashing {
		t.Error("should not thrash after 1 reject")
	}

	// Second reject
	ma.RecordReject("m1")
	m = ma.Get("m1")
	result = td.Check(m)
	if !result.Thrashing {
		t.Fatal("should thrash after 2 rejects")
	}

	// Complete — status stays thrashing (not overwritten)
	ma.CompleteMission("m1")
	m = ma.Get("m1")
	if m.Status != "thrashing" {
		t.Errorf("status = %q, should remain thrashing", m.Status)
	}

	// Check events
	hasMetrics := false
	hasThrash := false
	for _, e := range *events {
		switch e["type"] {
		case "metrics_snapshot":
			hasMetrics = true
		case "mission_thrashing":
			hasThrash = true
		}
	}
	if !hasMetrics {
		t.Error("expected metrics_snapshot event")
	}
	if !hasThrash {
		t.Error("expected mission_thrashing event")
	}
}

func TestMeterThrashPFIntegration(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	config := ThrashConfig{
		PFSoftLimit:        3,
		PFConsecutiveTurns: 2,
		MaxRejects:         100,
		MaxConflicts:       100,
		MaxTestFailures:    100,
	}
	td := NewThrashDetector(config, client)

	ma.StartMission("m1")

	// Turn 1: 4 PFs (over limit of 3)
	for i := 0; i < 4; i++ {
		ma.RecordPF("m1", 100)
	}
	ma.EndTurn("m1")

	m := ma.Get("m1")
	result := td.Check(m)
	if result.Thrashing {
		t.Error("should not thrash after 1 turn over limit")
	}

	// Turn 2: 5 PFs (over limit again — 2 consecutive)
	for i := 0; i < 5; i++ {
		ma.RecordPF("m1", 100)
	}
	ma.EndTurn("m1")

	m = ma.Get("m1")
	result = td.Check(m)
	if !result.Thrashing {
		t.Fatal("should thrash after 2 consecutive turns over PF limit")
	}
	if !strings.Contains(result.Reason, "PF count exceeded") {
		t.Errorf("reason = %q", result.Reason)
	}
}

func TestMetricsSummaryWithThrash(t *testing.T) {
	ts, _ := startMeterHeaven(t)
	client := NewHeavenClient(ts.URL)
	ma := NewMetricsAggregator(client)

	config := DefaultThrashConfig()
	config.MaxRejects = 1
	td := NewThrashDetector(config, client)

	ma.StartMission("m1")
	ma.RecordReject("m1")

	m := ma.Get("m1")
	td.Check(m)

	s := ma.Summary("m1")
	if !strings.Contains(s, "thrashing") {
		t.Errorf("summary should show thrashing status: %s", s)
	}
	if !strings.Contains(s, "thrash:") {
		t.Errorf("summary should show thrash reason: %s", s)
	}
}
