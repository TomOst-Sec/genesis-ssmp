package god

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock Oracle Provider
// ---------------------------------------------------------------------------

// mockOracleProvider returns a fixed OracleResponse as raw JSON.
type mockOracleProvider struct {
	response  []byte
	sendErr   error
	callCount int
	mu        sync.Mutex
	lastPack  *MissionPack
}

func (m *mockOracleProvider) Send(pack *MissionPack) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	m.lastPack = pack
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	return m.response, nil
}

func (m *mockOracleProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// validOracleResponse creates a valid OracleResponse JSON.
func validOracleResponse() []byte {
	resp := OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID:   "oracle-plan-001",
			TaskDesc: "Resolved thrashing task",
			RepoPath: "/repo",
			Nodes: []DAGNode{
				{
					Mission: Mission{
						MissionID:   "oracle-m1",
						Goal:        "Fix the root cause",
						BaseRev:     "HEAD",
						Scopes:      []Scope{{ScopeType: "file", ScopeValue: "main.go"}},
						LeaseIDs:    []string{},
						Tasks:       []string{"fix"},
						TokenBudget: 8000,
						CreatedAt:   time.Now().UTC().Format(time.RFC3339),
					},
					DependsOn: []string{},
				},
				{
					Mission: Mission{
						MissionID:   "oracle-m2",
						Goal:        "Update tests for new approach",
						BaseRev:     "HEAD",
						Scopes:      []Scope{{ScopeType: "file", ScopeValue: "main_test.go"}},
						LeaseIDs:    []string{},
						Tasks:       []string{"test"},
						TokenBudget: 4000,
						CreatedAt:   time.Now().UTC().Format(time.RFC3339),
					},
					DependsOn: []string{"oracle-m1"},
				},
			},
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		LeasesPlan: []Scope{
			{ScopeType: "file", ScopeValue: "main.go"},
			{ScopeType: "file", ScopeValue: "main_test.go"},
		},
		RiskHotspots:     []string{"main.go:42", "utils.go:10"},
		RecommendedTests: []string{"go test ./...", "go test -run TestMain"},
	}
	data, _ := json.Marshal(resp)
	return data
}

func startOracleHeaven(t *testing.T) (*httptest.Server, *[]map[string]any) {
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
// Oracle disabled tests
// ---------------------------------------------------------------------------

func TestOracleDisabledReturnsNil(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	o := NewOracle(OracleConfig{Enabled: false}, client)

	resp, err := o.Escalate(OracleRequest{
		SpecBlobID:      "spec-001",
		SymbolShortlist: []string{"Foo"},
		MetricsSummary:  &MissionMetrics{MissionID: "m1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response when oracle is disabled")
	}
}

func TestOracleNoProviderReturnsNil(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	o := NewOracle(OracleConfig{Enabled: true, Provider: nil}, client)

	resp, err := o.Escalate(OracleRequest{
		SpecBlobID:      "spec-001",
		SymbolShortlist: []string{},
		MetricsSummary:  &MissionMetrics{MissionID: "m1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response when no provider configured")
	}
}

// ---------------------------------------------------------------------------
// Oracle escalation tests
// ---------------------------------------------------------------------------

func TestOracleEscalateSuccess(t *testing.T) {
	ts, events := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	provider := &mockOracleProvider{response: validOracleResponse()}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	resp, err := o.Escalate(OracleRequest{
		SpecBlobID:      "spec-001",
		SymbolShortlist: []string{"Foo", "Bar"},
		MetricsSummary: &MissionMetrics{
			MissionID:    "m1",
			Status:       "thrashing",
			ThrashReason: "patch rejected 2 times",
			Rejects:      2,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify updated DAG
	if resp.UpdatedDAG.PlanID != "oracle-plan-001" {
		t.Errorf("PlanID = %q", resp.UpdatedDAG.PlanID)
	}
	if len(resp.UpdatedDAG.Nodes) != 2 {
		t.Errorf("Nodes count = %d, want 2", len(resp.UpdatedDAG.Nodes))
	}
	if resp.UpdatedDAG.Nodes[0].Mission.MissionID != "oracle-m1" {
		t.Errorf("first mission ID = %q", resp.UpdatedDAG.Nodes[0].Mission.MissionID)
	}

	// Verify leases plan
	if len(resp.LeasesPlan) != 2 {
		t.Errorf("LeasesPlan count = %d", len(resp.LeasesPlan))
	}

	// Verify risk hotspots
	if len(resp.RiskHotspots) != 2 {
		t.Errorf("RiskHotspots count = %d", len(resp.RiskHotspots))
	}

	// Verify recommended tests
	if len(resp.RecommendedTests) != 2 {
		t.Errorf("RecommendedTests count = %d", len(resp.RecommendedTests))
	}

	// Verify event logged
	found := false
	for _, e := range *events {
		if e["type"] == "oracle_escalation" {
			found = true
			if e["spec_blob_id"] != "spec-001" {
				t.Errorf("event spec_blob_id = %v", e["spec_blob_id"])
			}
			if e["new_plan_id"] != "oracle-plan-001" {
				t.Errorf("event new_plan_id = %v", e["new_plan_id"])
			}
		}
	}
	if !found {
		t.Error("expected oracle_escalation event")
	}

	// Verify provider was called once
	if provider.CallCount() != 1 {
		t.Errorf("provider called %d times, want 1", provider.CallCount())
	}
}

func TestOracleEscalateProviderError(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	provider := &mockOracleProvider{sendErr: fmt.Errorf("network timeout")}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	_, err := o.Escalate(OracleRequest{
		SpecBlobID:      "spec-001",
		SymbolShortlist: []string{},
		MetricsSummary:  &MissionMetrics{MissionID: "m1"},
	})
	if err == nil {
		t.Fatal("expected error on provider failure")
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("error = %v", err)
	}
}

func TestOracleEscalateInvalidJSON(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	provider := &mockOracleProvider{response: []byte("not json")}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	_, err := o.Escalate(OracleRequest{
		SpecBlobID:      "spec-001",
		SymbolShortlist: []string{},
		MetricsSummary:  &MissionMetrics{MissionID: "m1"},
	})
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Oracle response validation tests
// ---------------------------------------------------------------------------

func TestValidateOracleResponseValid(t *testing.T) {
	var resp OracleResponse
	json.Unmarshal(validOracleResponse(), &resp)

	if err := validateOracleResponse(&resp); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestValidateOracleResponseMissingDAG(t *testing.T) {
	resp := &OracleResponse{
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "missing updated_dag") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseEmptyPlanID(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG:       &MissionDAG{Nodes: []DAGNode{{Mission: Mission{MissionID: "m1", Goal: "g"}}}},
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "plan_id") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseNilNodes(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG:       &MissionDAG{PlanID: "p1"},
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "nodes must not be nil") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseEmptyNodes(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG:       &MissionDAG{PlanID: "p1", Nodes: []DAGNode{}},
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "nodes must not be empty") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseNodeMissingMissionID(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "p1",
			Nodes:  []DAGNode{{Mission: Mission{Goal: "g"}}},
		},
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "mission_id is empty") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseNodeMissingGoal(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "p1",
			Nodes:  []DAGNode{{Mission: Mission{MissionID: "m1"}}},
		},
		LeasesPlan:       []Scope{},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "goal is empty") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseMissingLeasesPlan(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "p1",
			Nodes:  []DAGNode{{Mission: Mission{MissionID: "m1", Goal: "g"}}},
		},
		RiskHotspots:     []string{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "missing leases_plan") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseMissingRiskHotspots(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "p1",
			Nodes:  []DAGNode{{Mission: Mission{MissionID: "m1", Goal: "g"}}},
		},
		LeasesPlan:       []Scope{},
		RecommendedTests: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "missing risk_hotspots") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateOracleResponseMissingRecommendedTests(t *testing.T) {
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "p1",
			Nodes:  []DAGNode{{Mission: Mission{MissionID: "m1", Goal: "g"}}},
		},
		LeasesPlan:   []Scope{},
		RiskHotspots: []string{},
	}
	err := validateOracleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "missing recommended_tests") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt template tests
// ---------------------------------------------------------------------------

func TestOraclePromptContainsStateVector(t *testing.T) {
	req := OracleRequest{
		SpecBlobID:      "spec-blob-abc",
		SymbolShortlist: []string{"Greet", "Hello"},
		MetricsSummary: &MissionMetrics{
			MissionID:    "m1",
			Status:       "thrashing",
			ThrashReason: "too many rejects",
		},
	}

	pack := buildOraclePrompt(req)

	if !strings.Contains(pack.Header, "Cloud God Oracle") {
		t.Error("prompt should contain oracle identity")
	}
	if !strings.Contains(pack.Header, "RESPOND WITH EXACTLY ONE JSON OBJECT") {
		t.Error("prompt should force JSON output")
	}
	if !strings.Contains(pack.Header, "spec-blob-abc") {
		t.Error("prompt should contain spec blob ID")
	}
	if !strings.Contains(pack.Header, "Greet") {
		t.Error("prompt should contain symbol shortlist")
	}
	if !strings.Contains(pack.Header, "thrashing") {
		t.Error("prompt should contain metrics summary")
	}
	if pack.Mission.TokenBudget != 16000 {
		t.Errorf("TokenBudget = %d, want 16000", pack.Mission.TokenBudget)
	}
	if !strings.HasPrefix(pack.Mission.MissionID, "oracle-") {
		t.Errorf("MissionID = %q, want oracle- prefix", pack.Mission.MissionID)
	}
}

// ---------------------------------------------------------------------------
// EscalateOnThrash tests
// ---------------------------------------------------------------------------

func TestEscalateOnThrashNotThrashing(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	provider := &mockOracleProvider{response: validOracleResponse()}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	// Active mission — not thrashing
	metrics := &MissionMetrics{MissionID: "m1", Status: "active"}
	resp, err := o.EscalateOnThrash("m1", metrics, "spec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("should not escalate non-thrashing mission")
	}
	if provider.CallCount() != 0 {
		t.Error("provider should not be called for non-thrashing mission")
	}
}

func TestEscalateOnThrashNilMetrics(t *testing.T) {
	ts, _ := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	o := NewOracle(OracleConfig{Enabled: true}, client)

	resp, err := o.EscalateOnThrash("m1", nil, "spec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("should not escalate with nil metrics")
	}
}

func TestEscalateOnThrashTriggersOracle(t *testing.T) {
	ts, events := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	provider := &mockOracleProvider{response: validOracleResponse()}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	metrics := &MissionMetrics{
		MissionID:    "m1",
		Status:       "thrashing",
		ThrashReason: "patch rejected 2 times",
		Rejects:      2,
		Turns:        5,
	}

	resp, err := o.EscalateOnThrash("m1", metrics, "spec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected oracle response")
	}

	// Verify we got the replacement plan
	if resp.UpdatedDAG.PlanID != "oracle-plan-001" {
		t.Errorf("PlanID = %q", resp.UpdatedDAG.PlanID)
	}
	if len(resp.UpdatedDAG.Nodes) != 2 {
		t.Errorf("Nodes = %d, want 2", len(resp.UpdatedDAG.Nodes))
	}

	// Verify event logged
	found := false
	for _, e := range *events {
		if e["type"] == "oracle_escalation" {
			found = true
		}
	}
	if !found {
		t.Error("expected oracle_escalation event")
	}
}

// ---------------------------------------------------------------------------
// Integration: Thrash → Oracle → Plan replacement
// ---------------------------------------------------------------------------

func TestThrashTriggersOracleAndPlanUpdate(t *testing.T) {
	ts, events := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	// Set up metering and thrash detection
	ma := NewMetricsAggregator(client)
	config := DefaultThrashConfig()
	config.MaxRejects = 2
	td := NewThrashDetector(config, client)

	// Set up oracle
	provider := &mockOracleProvider{response: validOracleResponse()}
	o := NewOracle(OracleConfig{Enabled: true, Provider: provider}, client)

	// Simulate mission execution
	ma.StartMission("m1")
	ma.RecordReject("m1")
	ma.RecordReject("m1") // trigger thrash

	metrics := ma.Get("m1")
	thrashResult := td.Check(metrics)
	if !thrashResult.Thrashing {
		t.Fatal("should be thrashing after 2 rejects")
	}

	// Now escalate to oracle
	oracleResp, err := o.EscalateOnThrash("m1", metrics, "spec-blob-123")
	if err != nil {
		t.Fatalf("oracle error: %v", err)
	}
	if oracleResp == nil {
		t.Fatal("expected oracle response for thrashing mission")
	}

	// Verify replacement plan is usable
	newDAG := oracleResp.UpdatedDAG
	if len(newDAG.Nodes) == 0 {
		t.Error("replacement DAG should have nodes")
	}
	if len(oracleResp.LeasesPlan) == 0 {
		t.Error("replacement plan should have leases")
	}
	if len(oracleResp.RiskHotspots) == 0 {
		t.Error("replacement plan should have risk hotspots")
	}
	if len(oracleResp.RecommendedTests) == 0 {
		t.Error("replacement plan should have recommended tests")
	}

	// Verify event trail: thrash event then oracle event
	hasThrash := false
	hasOracle := false
	for _, e := range *events {
		switch e["type"] {
		case "mission_thrashing":
			hasThrash = true
		case "oracle_escalation":
			hasOracle = true
		}
	}
	if !hasThrash {
		t.Error("expected mission_thrashing event")
	}
	if !hasOracle {
		t.Error("expected oracle_escalation event")
	}
}

// ---------------------------------------------------------------------------
// State vector + cyclic dependency tests
// ---------------------------------------------------------------------------

func TestOraclePromptStateVectorContainsAllFields(t *testing.T) {
	metrics := &MissionMetrics{
		MissionID: "m-vec-1",
		Status:    "thrashing",
		Turns:     5,
		Rejects:   3,
		TokensIn:  12000,
		TokensOut: 4000,
	}

	req := OracleRequest{
		SpecBlobID:           "spec-blob-abc",
		DecisionLedgerBlobID: "ledger-blob-xyz",
		FailingTestBlobID:    "test-blob-fail",
		SymbolShortlist:      []string{"Foo", "Bar", "Baz"},
		MetricsSummary:       metrics,
	}

	pack := buildOraclePrompt(req)

	// The pack header must contain the oracle prompt template
	if !strings.Contains(pack.Header, "Cloud God Oracle") {
		t.Error("pack header missing oracle prompt")
	}
	if !strings.Contains(pack.Header, "STATE VECTOR") {
		t.Error("pack header missing STATE VECTOR marker")
	}

	// The state vector JSON must contain all OracleRequest fields
	stateVec := pack.Header
	for _, field := range []string{
		`"spec_blob_id":"spec-blob-abc"`,
		`"decision_ledger_blob_id":"ledger-blob-xyz"`,
		`"failing_test_blob_id":"test-blob-fail"`,
		`"symbol_shortlist":["Foo","Bar","Baz"]`,
		`"metrics_summary"`,
		`"mission_id":"m-vec-1"`,
		`"turns":5`,
		`"rejects":3`,
		`"tokens_in":12000`,
		`"tokens_out":4000`,
	} {
		if !strings.Contains(stateVec, field) {
			t.Errorf("state vector missing field: %s", field)
		}
	}

	// Mission in the pack should have oracle- prefix
	if !strings.HasPrefix(pack.Mission.MissionID, "oracle-") {
		t.Errorf("oracle mission ID should have oracle- prefix, got %s", pack.Mission.MissionID)
	}
	if pack.Mission.TokenBudget != 16000 {
		t.Errorf("oracle token budget = %d, want 16000", pack.Mission.TokenBudget)
	}
}

func TestOracleResponseValidationCyclicDeps(t *testing.T) {
	// A -> B -> C -> A (cycle)
	resp := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID:   "cyclic-plan",
			TaskDesc: "Has a cycle",
			Nodes: []DAGNode{
				{Mission: Mission{MissionID: "a", Goal: "A"}, DependsOn: []string{"c"}},
				{Mission: Mission{MissionID: "b", Goal: "B"}, DependsOn: []string{"a"}},
				{Mission: Mission{MissionID: "c", Goal: "C"}, DependsOn: []string{"b"}},
			},
		},
		LeasesPlan:       []Scope{{ScopeType: "file", ScopeValue: "x.go"}},
		RiskHotspots:     []string{"x.go"},
		RecommendedTests: []string{"go test"},
	}

	err := validateOracleResponse(resp)
	if err == nil {
		t.Fatal("expected error for cyclic dependencies")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("error should mention cyclic, got: %v", err)
	}

	// Self-cycle: A -> A
	resp2 := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "self-cycle",
			Nodes: []DAGNode{
				{Mission: Mission{MissionID: "x", Goal: "X"}, DependsOn: []string{"x"}},
			},
		},
		LeasesPlan:       []Scope{{ScopeType: "file", ScopeValue: "y.go"}},
		RiskHotspots:     []string{"y.go"},
		RecommendedTests: []string{"go test"},
	}

	err = validateOracleResponse(resp2)
	if err == nil {
		t.Fatal("expected error for self-cycle")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("error should mention cyclic for self-cycle, got: %v", err)
	}

	// Valid DAG: A -> B (no cycle)
	resp3 := &OracleResponse{
		UpdatedDAG: &MissionDAG{
			PlanID: "valid-dag",
			Nodes: []DAGNode{
				{Mission: Mission{MissionID: "a", Goal: "A"}, DependsOn: []string{}},
				{Mission: Mission{MissionID: "b", Goal: "B"}, DependsOn: []string{"a"}},
			},
		},
		LeasesPlan:       []Scope{{ScopeType: "file", ScopeValue: "z.go"}},
		RiskHotspots:     []string{"z.go"},
		RecommendedTests: []string{"go test"},
	}

	if err := validateOracleResponse(resp3); err != nil {
		t.Errorf("valid DAG should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TSLN Lean mode tests
// ---------------------------------------------------------------------------

func TestOraclePromptLeanMode(t *testing.T) {
	t.Setenv("GENESIS_LEAN", "1")

	req := OracleRequest{
		SpecBlobID:      "spec-blob-abc",
		SymbolShortlist: []string{"Foo", "Bar"},
		MetricsSummary: &MissionMetrics{
			MissionID:      "m-lean-1",
			Status:         "thrashing",
			PFCount:        14,
			PFResponseSize: 12100,
			Retries:        2,
			Rejects:        2,
			Conflicts:      0,
			TestFailures:   1,
			TokensIn:       3500,
			TokensOut:      750,
			Turns:          4,
			ElapsedMS:      5200,
			StartedAt:      time.Now().UTC().Format(time.RFC3339),
		},
	}

	pack := buildOraclePrompt(req)

	// Must contain TSLN header and format note
	if !strings.Contains(pack.Header, "TSLN/2.0") {
		t.Error("lean mode should produce TSLN header")
	}
	if !strings.Contains(pack.Header, "METRICS (TSLN)") {
		t.Error("lean mode should contain METRICS (TSLN) section")
	}
	if !strings.Contains(pack.Header, "pipe-delimited") {
		t.Error("lean mode should contain TSLN format note")
	}

	// Must still contain JSON envelope for non-metrics fields
	if !strings.Contains(pack.Header, "spec-blob-abc") {
		t.Error("lean mode should preserve spec_blob_id in JSON envelope")
	}
	if !strings.Contains(pack.Header, "Foo") {
		t.Error("lean mode should preserve symbol_shortlist in JSON envelope")
	}

	// Must contain pipe-delimited data row with metrics values
	if !strings.Contains(pack.Header, "thrashing") {
		t.Error("lean mode TSLN should contain status value")
	}
	if !strings.Contains(pack.Header, "|") {
		t.Error("lean mode TSLN should contain pipe delimiters")
	}
}

func TestOraclePromptLeanFallback(t *testing.T) {
	t.Setenv("GENESIS_LEAN", "0")

	req := OracleRequest{
		SpecBlobID:      "spec-blob-fallback",
		SymbolShortlist: []string{"Baz"},
		MetricsSummary: &MissionMetrics{
			MissionID: "m-fallback",
			Status:    "thrashing",
			Rejects:   2,
		},
	}

	pack := buildOraclePrompt(req)

	// Should NOT contain TSLN markers
	if strings.Contains(pack.Header, "TSLN/2.0") {
		t.Error("non-lean mode should not contain TSLN header")
	}
	if strings.Contains(pack.Header, "METRICS (TSLN)") {
		t.Error("non-lean mode should not contain METRICS (TSLN) section")
	}

	// Should contain standard JSON fields
	if !strings.Contains(pack.Header, `"metrics_summary"`) {
		t.Error("non-lean mode should contain JSON metrics_summary")
	}
	if !strings.Contains(pack.Header, `"spec_blob_id"`) {
		t.Error("non-lean mode should contain JSON spec_blob_id")
	}
}

func TestOraclePromptLeanTokenSavings(t *testing.T) {
	metrics := &MissionMetrics{
		MissionID:        "m-savings",
		Status:           "thrashing",
		PFCount:          14,
		PFResponseSize:   12100,
		Retries:          2,
		Rejects:          2,
		Conflicts:        0,
		TestFailures:     1,
		TokensIn:         3500,
		TokensOut:        750,
		Turns:            4,
		ElapsedMS:        5200,
		PhaseTransitions: 1,
		ThrashReason:     "patch rejected 2 times",
		StartedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	req := OracleRequest{
		SpecBlobID:           "spec-blob-savings",
		DecisionLedgerBlobID: "ledger-blob-savings",
		FailingTestBlobID:    "test-blob-savings",
		SymbolShortlist:      []string{"Handler", "Router", "Middleware"},
		MetricsSummary:       metrics,
	}

	// Build JSON version
	t.Setenv("GENESIS_LEAN", "0")
	jsonPack := buildOraclePrompt(req)
	jsonSize := len(jsonPack.Header)

	// Build TSLN version
	t.Setenv("GENESIS_LEAN", "1")
	tslnPack := buildOraclePrompt(req)
	tslnSize := len(tslnPack.Header)

	reduction := (1.0 - float64(tslnSize)/float64(jsonSize)) * 100

	t.Logf("Oracle prompt — JSON: %d bytes (~%d tokens), TSLN: %d bytes (~%d tokens), reduction: %.1f%%",
		jsonSize, jsonSize/4, tslnSize, tslnSize/4, reduction)

	// Single-snapshot blended case (JSON envelope + TSLN metrics) is roughly break-even.
	// TSLN overhead (header + format note) is amortized over multiple rows.
	// Assert the TSLN version is within 15% of JSON size (no blowup).
	overhead := float64(tslnSize-jsonSize) / float64(jsonSize) * 100
	if overhead > 15 {
		t.Errorf("TSLN overhead %.1f%% exceeds 15%% tolerance (json=%d, tsln=%d)", overhead, jsonSize, tslnSize)
	}
}

func TestThrashOracleDisabledNoEscalation(t *testing.T) {
	ts, events := startOracleHeaven(t)
	client := NewHeavenClient(ts.URL)

	ma := NewMetricsAggregator(client)
	config := DefaultThrashConfig()
	config.MaxRejects = 1
	td := NewThrashDetector(config, client)

	// Oracle disabled
	o := NewOracle(OracleConfig{Enabled: false}, client)

	ma.StartMission("m1")
	ma.RecordReject("m1")

	metrics := ma.Get("m1")
	td.Check(metrics) // triggers thrash

	resp, err := o.EscalateOnThrash("m1", metrics, "spec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("oracle should not respond when disabled")
	}

	// No oracle event
	for _, e := range *events {
		if e["type"] == "oracle_escalation" {
			t.Error("should not have oracle_escalation event when disabled")
		}
	}
}
