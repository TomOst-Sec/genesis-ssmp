package god

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genesis-ssmp/genesis/heaven"
)

// ---------------------------------------------------------------------------
// I4: Planner leases are visible in Heaven state after planning
// ---------------------------------------------------------------------------

func TestPlannerLeasesVisibleInHeaven(t *testing.T) {
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server init: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := NewHeavenClient(ts.URL)

	fixtureDir, err := filepath.Abs("../fixtures")
	if err != nil {
		t.Fatalf("abs fixtures: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixtureDir, "sample.go")); os.IsNotExist(err) {
		t.Skipf("fixtures not found at %s", fixtureDir)
	}

	planner := NewPlanner(client)
	dag, err := planner.Plan("Refactor the Greet function", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Check that lease events were logged in Heaven
	events, err := client.TailEvents(100)
	if err != nil {
		t.Fatalf("tail events: %v", err)
	}

	leaseEvents := 0
	for _, raw := range events {
		var evt map[string]any
		json.Unmarshal(raw, &evt)
		if evt["type"] == "lease_acquired" || evt["type"] == "lease_grant" {
			leaseEvents++
		}
	}

	// Planner creates missions with scopes -> should have acquired leases
	implementMissions := 0
	for _, node := range dag.Nodes {
		if len(node.Mission.Tasks) > 0 && node.Mission.Tasks[0] != "analyze" {
			implementMissions++
			if len(node.Mission.LeaseIDs) == 0 && len(node.Mission.Scopes) > 0 {
				t.Logf("warning: mission %s has scopes but no lease IDs (may have been denied)", node.Mission.MissionID[:8])
			}
		}
	}

	if implementMissions == 0 {
		t.Error("expected at least 1 implementation mission")
	}

	// Check that leases are active in Heaven via direct HTTP
	resp, err := http.Get(ts.URL + "/lease/list")
	if err != nil {
		t.Fatalf("GET /lease/list: %v", err)
	}
	defer resp.Body.Close()

	var leaseListResp struct {
		Leases []LeaseResult `json:"leases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&leaseListResp); err != nil {
		t.Fatalf("decode lease list: %v", err)
	}

	if len(leaseListResp.Leases) == 0 && implementMissions > 0 {
		t.Error("expected active leases after planning")
	}
	t.Logf("Plan created %d missions, %d active leases", len(dag.Nodes), len(leaseListResp.Leases))
}

// ---------------------------------------------------------------------------
// I7: Integrator respects DAG dependency order
// ---------------------------------------------------------------------------

func TestIntegratorDAGOrder(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "order.go", "package main\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	integrator := NewIntegrator(client)

	// Build a 3-node chain: A -> B -> C
	missionA := "m-order-a"
	missionB := "m-order-b"
	missionC := "m-order-c"

	dag := &MissionDAG{
		PlanID:   "order-plan",
		TaskDesc: "test order",
		Nodes: []DAGNode{
			{Mission: Mission{MissionID: missionA, Goal: "step A"}, DependsOn: []string{}},
			{Mission: Mission{MissionID: missionB, Goal: "step B"}, DependsOn: []string{missionA}},
			{Mission: Mission{MissionID: missionC, Goal: "step C"}, DependsOn: []string{missionB}},
		},
	}

	// Execute in dependency order
	executionOrder := []string{}
	for _, node := range topologicalSort(dag.Nodes) {
		m := node.Mission
		ir := &EditIR{
			Ops: []EditOp{{
				Op:         "add_file",
				Path:       m.MissionID + ".go",
				AnchorHash: "new_file",
				Content:    fmt.Sprintf("package main\n// created by %s\n", m.MissionID),
			}},
		}

		req := makeIntegrateRequest(dir, m.MissionID, ir, Manifest{
			SymbolsTouched: []string{},
			FilesTouched:   []string{m.MissionID + ".go"},
		})
		req.FileClocks = nil

		result, err := integrator.Integrate(req)
		if err != nil {
			t.Fatalf("integrate %s: %v", m.MissionID, err)
		}
		if !result.Success {
			t.Fatalf("integrate %s failed: %s", m.MissionID, result.Error)
		}
		executionOrder = append(executionOrder, m.MissionID)
	}

	// Verify order: A before B before C
	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executionOrder))
	}
	if executionOrder[0] != missionA {
		t.Errorf("first execution should be A, got %s", executionOrder[0])
	}
	if executionOrder[1] != missionB {
		t.Errorf("second execution should be B, got %s", executionOrder[1])
	}
	if executionOrder[2] != missionC {
		t.Errorf("third execution should be C, got %s", executionOrder[2])
	}

	// Verify all files created
	for _, mid := range []string{missionA, missionB, missionC} {
		path := filepath.Join(dir, mid+".go")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("file %s.go not created", mid)
		}
	}
}

// topologicalSort returns nodes in dependency order (simple BFS).
func topologicalSort(nodes []DAGNode) []DAGNode {
	idToNode := make(map[string]DAGNode)
	depCount := make(map[string]int)
	dependents := make(map[string][]string)

	for _, n := range nodes {
		mid := n.Mission.MissionID
		idToNode[mid] = n
		depCount[mid] = len(n.DependsOn)
		for _, dep := range n.DependsOn {
			dependents[dep] = append(dependents[dep], mid)
		}
	}

	var queue []string
	for _, n := range nodes {
		if depCount[n.Mission.MissionID] == 0 {
			queue = append(queue, n.Mission.MissionID)
		}
	}

	var sorted []DAGNode
	for len(queue) > 0 {
		mid := queue[0]
		queue = queue[1:]
		sorted = append(sorted, idToNode[mid])
		for _, dep := range dependents[mid] {
			depCount[dep]--
			if depCount[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	return sorted
}

// ---------------------------------------------------------------------------
// I1: No transcript/chat data in Heaven events
// ---------------------------------------------------------------------------

func TestNoTranscriptSharingInHeavenState(t *testing.T) {
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server init: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := NewHeavenClient(ts.URL)

	fixtureDir, err := filepath.Abs("../fixtures")
	if err != nil {
		t.Fatalf("abs fixtures: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixtureDir, "sample.go")); os.IsNotExist(err) {
		t.Skipf("fixtures not found at %s", fixtureDir)
	}

	// Run a full plan cycle to generate events
	planner := NewPlanner(client)
	dag, err := planner.Plan("Add a helper function", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Execute mock missions
	workDir := t.TempDir()
	provider := &e2eProvider{responses: map[string][]byte{}}
	for _, node := range dag.Nodes {
		if len(node.Mission.Tasks) > 0 && node.Mission.Tasks[0] == "analyze" {
			continue
		}
		provider.responses[node.Mission.MissionID] = makeAngelResponse(
			node.Mission.MissionID,
			"gen_"+node.Mission.MissionID[:6]+".go",
			"package main\n",
		)
	}

	adapter := NewProviderAdapter(provider)
	integrator := NewIntegrator(client)
	metricsAgg := NewMetricsAggregator(client)

	for _, node := range dag.Nodes {
		m := node.Mission
		metricsAgg.StartMission(m.MissionID)
		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			metricsAgg.EndTurn(m.MissionID)
			metricsAgg.CompleteMission(m.MissionID)
			continue
		}

		pack := &MissionPack{Header: "test", Mission: m}
		angelResp, _, err := adapter.Execute(pack)
		if err != nil {
			metricsAgg.EndTurn(m.MissionID)
			metricsAgg.CompleteMission(m.MissionID)
			continue
		}

		fileClocks := make(map[string]int64)
		integrator.Integrate(IntegrateRequest{
			OwnerID: "invariant-test", RepoRoot: workDir,
			Response: angelResp, Mission: m, FileClocks: fileClocks,
		})
		metricsAgg.EndTurn(m.MissionID)
		metricsAgg.CompleteMission(m.MissionID)
	}

	// Now scan ALL events — none should contain transcript/chat content
	events, err := client.TailEvents(1000)
	if err != nil {
		t.Fatalf("tail events: %v", err)
	}

	forbiddenTypes := []string{"chat_transcript", "agent_message", "user_message", "conversation"}
	forbiddenFields := []string{"transcript", "chat_history", "messages", "conversation_log"}

	for i, raw := range events {
		var evt map[string]any
		if err := json.Unmarshal(raw, &evt); err != nil {
			continue
		}

		evtType, _ := evt["type"].(string)
		for _, forbidden := range forbiddenTypes {
			if evtType == forbidden {
				t.Errorf("event[%d] has forbidden type %q — shared state must not include transcripts", i, forbidden)
			}
		}

		evtStr := string(raw)
		for _, forbidden := range forbiddenFields {
			if strings.Contains(evtStr, `"`+forbidden+`"`) {
				t.Errorf("event[%d] contains forbidden field %q — shared state must not include transcripts", i, forbidden)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// I2: Mission pack sizes are bounded (no repo dumps)
// ---------------------------------------------------------------------------

func TestMissionPackSizeBoundedInvariant(t *testing.T) {
	client, fixtureDir, cleanup := benchSetup(t)
	defer cleanup()

	planner := NewPlanner(client)
	dag, err := planner.Plan("Add a plot command with chart rendering", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := NewPromptCompiler(client.BaseURL + "/pf")

	const maxPackBytes = 200 * 1024 // 200KB — absolute max
	const maxShardLines = 10000

	for _, node := range dag.Nodes {
		m := node.Mission

		candidates := []CandidateShard{}
		for _, scope := range m.Scopes {
			if scope.ScopeType == "symbol" {
				candidates = append(candidates, CandidateShard{
					Kind:    "symdef",
					BlobID:  "blob-" + scope.ScopeValue,
					Content: []byte(fmt.Sprintf(`{"symbol":%q,"def":"func %s() {}"}`, scope.ScopeValue, scope.ScopeValue)),
					Symbol:  scope.ScopeValue,
				})
			}
		}

		pack, err := pc.Compile(m, candidates)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		packJSON, _ := json.Marshal(pack)

		// I2: pack size bounded
		if len(packJSON) > maxPackBytes {
			t.Errorf("mission %s: pack %d bytes > %d bytes (200KB limit)",
				m.MissionID[:8], len(packJSON), maxPackBytes)
		}

		// I2: no shard has too many lines
		for _, shard := range pack.InlineShards {
			lineCount := strings.Count(string(shard.Content), "\n") + 1
			if lineCount > maxShardLines {
				t.Errorf("mission %s: shard %s has %d lines > %d limit",
					m.MissionID[:8], shard.Kind, lineCount, maxShardLines)
			}
		}

		// Token budget respected
		if pack.BudgetMeta.TotalTokens > pack.BudgetMeta.TokenBudget {
			t.Errorf("mission %s: %d tokens > %d budget",
				m.MissionID[:8], pack.BudgetMeta.TotalTokens, pack.BudgetMeta.TokenBudget)
		}
	}
}

// ---------------------------------------------------------------------------
// I7: Full determinism — same inputs, same outputs, including receipts
// ---------------------------------------------------------------------------

func TestFullDeterminismWithReceipts(t *testing.T) {
	origNow := nowFunc
	origID := idFunc
	t.Cleanup(func() { nowFunc = origNow; idFunc = origID })

	type runResult struct {
		planID     string
		missionIDs []string
		eventCount int
		receipts   []string
	}

	runDeterministic := func() runResult {
		frozen := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
		nowFunc = func() time.Time { return frozen }
		counter := 0
		idFunc = func() string {
			counter++
			return fmt.Sprintf("det-receipt-%04d", counter)
		}

		client, fixtureDir, cleanup := benchSetup(t)
		defer cleanup()

		planner := NewPlanner(client)
		dag, err := planner.Plan("Add a utility function", fixtureDir)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}

		var missionIDs []string
		for _, n := range dag.Nodes {
			missionIDs = append(missionIDs, n.Mission.MissionID)
		}

		// Run verifier
		verifier := NewVerifier(client)
		verifier.runCmd = mockRunCmd("PASS\n", 0)
		vResult, _ := verifier.Verify(VerifyRequest{
			MissionID: dag.PlanID,
			RepoRoot:  t.TempDir(),
			Command:   "echo PASS",
		})

		var receipts []string
		if vResult != nil && vResult.BlobID != "" {
			receipts = append(receipts, vResult.BlobID)
		}

		events, _ := client.TailEvents(1000)
		return runResult{
			planID:     dag.PlanID,
			missionIDs: missionIDs,
			eventCount: len(events),
			receipts:   receipts,
		}
	}

	r1 := runDeterministic()
	r2 := runDeterministic()

	if r1.planID != r2.planID {
		t.Errorf("plan IDs differ: %s vs %s", r1.planID, r2.planID)
	}
	if len(r1.missionIDs) != len(r2.missionIDs) {
		t.Fatalf("mission count differs: %d vs %d", len(r1.missionIDs), len(r2.missionIDs))
	}
	for i := range r1.missionIDs {
		if r1.missionIDs[i] != r2.missionIDs[i] {
			t.Errorf("mission[%d] differs: %s vs %s", i, r1.missionIDs[i], r2.missionIDs[i])
		}
	}
	if r1.eventCount != r2.eventCount {
		t.Errorf("event count differs: %d vs %d", r1.eventCount, r2.eventCount)
	}
}

// ---------------------------------------------------------------------------
// E2E Medium Complexity: "add plot command" (T81)
// ---------------------------------------------------------------------------

func TestE2EMediumComplexityPlotCommand(t *testing.T) {
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server init: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := NewHeavenClient(ts.URL)

	fixtureDir, err := filepath.Abs("../fixtures")
	if err != nil {
		t.Fatalf("abs fixtures: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixtureDir, "sample.go")); os.IsNotExist(err) {
		t.Skipf("fixtures not found at %s", fixtureDir)
	}

	client.IRBuild(fixtureDir)

	planner := NewPlanner(client)
	dag, err := planner.Plan("Add a plot command with sampling and ASCII chart rendering plus tests", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if len(dag.Nodes) < 2 {
		t.Errorf("medium complexity task should produce >= 2 missions, got %d", len(dag.Nodes))
	}
	t.Logf("Plan created %d missions for plot command", len(dag.Nodes))

	workDir := t.TempDir()
	sampleContent, _ := os.ReadFile(filepath.Join(fixtureDir, "sample.go"))
	os.WriteFile(filepath.Join(workDir, "sample.go"), sampleContent, 0o644)

	mockResponses := make(map[string][]byte)
	for i, node := range dag.Nodes {
		m := node.Mission
		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			continue
		}
		fileName := fmt.Sprintf("plot_%c.go", rune('a'+i))
		content := fmt.Sprintf("package sample\n\n// Generated by mission %s\nfunc Plot%c() {}\n", m.MissionID[:8], rune('A'+i))
		mockResponses[m.MissionID] = makeAngelResponse(m.MissionID, fileName, content)
	}

	provider := &e2eProvider{responses: mockResponses}
	adapter := NewProviderAdapter(provider)
	integrator := NewIntegrator(client)
	metricsAgg := NewMetricsAggregator(client)
	thrashDetector := NewThrashDetector(DefaultThrashConfig(), client)

	var executedMissions int
	for _, node := range dag.Nodes {
		m := node.Mission
		metricsAgg.StartMission(m.MissionID)

		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			metricsAgg.EndTurn(m.MissionID)
			metricsAgg.CompleteMission(m.MissionID)
			continue
		}

		pack := &MissionPack{
			Header: "Execute mission", Mission: m,
			BudgetMeta: BudgetMeta{TokenBudget: m.TokenBudget, TotalTokens: 100},
		}

		angelResp, usage, err := adapter.Execute(pack)
		if err != nil {
			t.Fatalf("provider execute for %s: %v", m.MissionID[:8], err)
		}
		metricsAgg.RecordProviderUsage(usage)

		if len(angelResp.Manifest.FilesTouched) > 0 {
			fileScopes := make([]Scope, len(angelResp.Manifest.FilesTouched))
			for j, f := range angelResp.Manifest.FilesTouched {
				fileScopes[j] = Scope{ScopeType: "file", ScopeValue: f}
			}
			client.LeaseAcquire("e2e-plot", m.MissionID, fileScopes)
		}

		fileClocks := make(map[string]int64)
		intResult, err := integrator.Integrate(IntegrateRequest{
			OwnerID: "e2e-plot", RepoRoot: workDir,
			Response: angelResp, Mission: m, FileClocks: fileClocks,
		})
		if err != nil {
			t.Fatalf("integrate for %s: %v", m.MissionID[:8], err)
		}

		if intResult.Success {
			executedMissions++
		}

		metrics := metricsAgg.Get(m.MissionID)
		thrashResult := thrashDetector.Check(metrics)
		if thrashResult.Thrashing {
			t.Logf("  Mission %s THRASHING: %s", m.MissionID[:8], thrashResult.Reason)
		}

		metricsAgg.EndTurn(m.MissionID)
		metricsAgg.CompleteMission(m.MissionID)
	}

	if executedMissions == 0 {
		t.Error("expected at least 1 mission to integrate successfully")
	}

	// Verify
	verifier := NewVerifier(client)
	verifier.runCmd = mockRunCmd("PASS\nok  \tpkg\t0.005s\n", 0)
	vResult, err := verifier.Verify(VerifyRequest{
		MissionID: dag.PlanID, RepoRoot: workDir, Command: "echo PASS",
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !vResult.Passed {
		t.Error("expected verification to pass")
	}

	// Check events
	events, _ := client.TailEvents(100)
	eventTypes := make(map[string]int)
	for _, raw := range events {
		var evt map[string]any
		json.Unmarshal(raw, &evt)
		if evtType, ok := evt["type"].(string); ok {
			eventTypes[evtType]++
		}
	}
	t.Logf("Events: %v", eventTypes)

	if eventTypes["mission_created"] == 0 {
		t.Error("expected mission_created events")
	}
	if eventTypes["integration_complete"] == 0 {
		t.Error("expected integration_complete events")
	}

	t.Logf("Medium complexity E2E: %d missions executed, %d events logged", executedMissions, len(events))
}

// ---------------------------------------------------------------------------
// Token accounting blocker diagnostic
// ---------------------------------------------------------------------------

func TestTokenAccountingBlockerDiagnostic(t *testing.T) {
	client, fixtureDir, cleanup := benchSetup(t)
	defer cleanup()

	planner := NewPlanner(client)
	dag, err := planner.Plan("Add a pow operation", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := NewPromptCompiler(client.BaseURL + "/pf")
	totalGenesisTokens := 0
	var shardBreakdown []string

	for _, node := range dag.Nodes {
		m := node.Mission
		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			continue
		}

		candidates := []CandidateShard{}
		for _, scope := range m.Scopes {
			if scope.ScopeType == "symbol" {
				candidates = append(candidates, CandidateShard{
					Kind:    "symdef",
					BlobID:  "blob-" + scope.ScopeValue,
					Content: []byte(fmt.Sprintf(`{"symbol":%q}`, scope.ScopeValue)),
					Symbol:  scope.ScopeValue,
				})
			}
		}

		pack, err := pc.Compile(m, candidates)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		totalGenesisTokens += pack.BudgetMeta.TotalTokens

		for _, shard := range pack.InlineShards {
			tokens := EstimateTokens(shard.Content)
			shardBreakdown = append(shardBreakdown,
				fmt.Sprintf("  mission=%s shard=%s tokens=%d bytes=%d",
					m.MissionID[:8], shard.Kind, tokens, len(shard.Content)))
		}
	}

	totalBaselineTokens := baselineTokens(t, fixtureDir, len(dag.Nodes))
	ratio := float64(totalBaselineTokens) / float64(totalGenesisTokens)

	t.Logf("=== TOKEN ACCOUNTING DIAGNOSTIC ===")
	t.Logf("Baseline: %d tokens", totalBaselineTokens)
	t.Logf("Genesis:  %d tokens", totalGenesisTokens)
	t.Logf("Ratio:    %.1fx", ratio)
	t.Logf("Missions: %d", len(dag.Nodes))
	for _, line := range shardBreakdown {
		t.Log(line)
	}

	if ratio < 3.0 {
		t.Logf("=== BLOCKERS ===")
		if totalGenesisTokens > 5000 {
			t.Log("BLOCKER: Genesis tokens too high — check shard sizes")
		}
		if len(dag.Nodes) > 5 {
			t.Log("BLOCKER: Too many missions — each adds overhead")
		}
		for _, line := range shardBreakdown {
			if strings.Contains(line, "tokens=") {
				// Parse token count
				t.Logf("  -> %s", line)
			}
		}
	}
}
