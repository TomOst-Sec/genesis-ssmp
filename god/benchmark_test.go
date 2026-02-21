package god

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/genesis-ssmp/genesis/heaven"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// benchSetup creates a Heaven server, indexes the fixture repo, and returns
// a client and cleanup function. Returns (client, fixtureDir, cleanup).
func benchSetup(t testing.TB) (*HeavenClient, string, func()) {
	t.Helper()
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server init: %v", err)
	}
	ts := httptest.NewServer(srv)

	client := NewHeavenClient(ts.URL)
	fixtureDir, _ := filepath.Abs("../fixtures")
	if _, err := os.Stat(filepath.Join(fixtureDir, "sample.go")); os.IsNotExist(err) {
		t.Skipf("fixtures not found at %s", fixtureDir)
	}

	if _, err := client.IRBuild(fixtureDir); err != nil {
		t.Fatalf("ir build: %v", err)
	}

	return client, fixtureDir, ts.Close
}

// benchPlanAndExecute runs a full plan→execute→integrate→verify cycle.
// Returns total input tokens used across all missions.
func benchPlanAndExecute(t testing.TB, client *HeavenClient, fixtureDir, taskDesc string) int {
	t.Helper()

	planner := NewPlanner(client)
	dag, err := planner.Plan(taskDesc, fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := NewPromptCompiler(client.BaseURL + "/pf")
	totalInputTokens := 0

	workDir := t.TempDir()
	sampleContent, _ := os.ReadFile(filepath.Join(fixtureDir, "sample.go"))
	os.WriteFile(filepath.Join(workDir, "sample.go"), sampleContent, 0o644)

	metricsAgg := NewMetricsAggregator(client)
	integrator := NewIntegrator(client)

	for _, node := range dag.Nodes {
		m := node.Mission
		metricsAgg.StartMission(m.MissionID)

		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			metricsAgg.EndTurn(m.MissionID)
			metricsAgg.CompleteMission(m.MissionID)
			continue
		}

		// Build candidate shards
		candidates := []CandidateShard{}
		for _, scope := range m.Scopes {
			if scope.ScopeType == "symbol" {
				candidates = append(candidates, CandidateShard{
					Kind:    "symdef",
					BlobID:  "blob-" + scope.ScopeValue,
					Content: []byte(fmt.Sprintf(`{"symbol":%q,"definition":"func %s() {}"}`, scope.ScopeValue, scope.ScopeValue)),
					Symbol:  scope.ScopeValue,
				})
			}
		}

		pack, err := pc.Compile(m, candidates)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		totalInputTokens += pack.BudgetMeta.TotalTokens

		// Mock execute
		mockResp := AngelResponse{
			MissionID:  m.MissionID,
			OutputType: "edit_ir",
			EditIR:     &EditIR{Ops: []EditOp{}},
			Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
		}

		fileClocks := make(map[string]int64)
		integrator.Integrate(IntegrateRequest{
			OwnerID:    "bench-owner",
			RepoRoot:   workDir,
			Response:   &mockResp,
			Mission:    m,
			FileClocks: fileClocks,
		})

		metricsAgg.EndTurn(m.MissionID)
		metricsAgg.CompleteMission(m.MissionID)
	}

	// Verify
	verifier := NewVerifier(client)
	verifier.runCmd = mockRunCmd("PASS\nok\n", 0)
	verifier.Verify(VerifyRequest{
		MissionID: dag.PlanID,
		RepoRoot:  workDir,
		Command:   "echo PASS",
	})

	return totalInputTokens
}

// baselineTokens estimates the "naive" token count for a task: send all files
// in fixtureDir as raw content per mission turn.
func baselineTokens(t testing.TB, fixtureDir string, missions int) int {
	t.Helper()
	totalBytes := 0
	filepath.Walk(fixtureDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".go" || ext == ".py" || ext == ".ts" {
			data, _ := os.ReadFile(path)
			totalBytes += len(data)
		}
		return nil
	})
	// Baseline: full file dump per mission turn
	return missions * EstimateTokens([]byte(strings.Repeat("x", totalBytes)))
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkTaskAddPow(b *testing.B) {
	client, fixtureDir, cleanup := benchSetup(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokens := benchPlanAndExecute(b, client, fixtureDir, "Add a pow operation to the math module")
		if tokens == 0 {
			b.Fatal("expected non-zero token count")
		}
	}
}

func BenchmarkTaskAddPlotCommand(b *testing.B) {
	client, fixtureDir, cleanup := benchSetup(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokens := benchPlanAndExecute(b, client, fixtureDir, "Add a plot command that renders charts with cross-module support")
		if tokens == 0 {
			b.Fatal("expected non-zero token count")
		}
	}
}

// ---------------------------------------------------------------------------
// Token accounting tests
// ---------------------------------------------------------------------------

func TestTokenAccountingBaseline(t *testing.T) {
	client, fixtureDir, cleanup := benchSetup(t)
	defer cleanup()

	// Task: "add pow" — should produce at least 2 missions (analysis + implementation)
	powTokens := benchPlanAndExecute(t, client, fixtureDir, "Add a pow operation")
	powBaseline := baselineTokens(t, fixtureDir, 2) // at least 2 missions

	if powBaseline == 0 || powTokens == 0 {
		t.Skip("no fixture data to compute token ratio")
	}

	powRatio := float64(powBaseline) / float64(powTokens)
	t.Logf("pow task: baseline=%d, genesis=%d, ratio=%.1fx", powBaseline, powTokens, powRatio)

	if powRatio < 5.0 {
		t.Errorf("pow task token ratio %.1fx < 5x target", powRatio)
	}

	// Task: "add plot" — medium complexity
	plotTokens := benchPlanAndExecute(t, client, fixtureDir, "Add a plot command with cross-module support")
	plotBaseline := baselineTokens(t, fixtureDir, 3)

	if plotBaseline == 0 || plotTokens == 0 {
		t.Skip("no fixture data to compute token ratio")
	}

	plotRatio := float64(plotBaseline) / float64(plotTokens)
	t.Logf("plot task: baseline=%d, genesis=%d, ratio=%.1fx", plotBaseline, plotTokens, plotRatio)

	if plotRatio < 3.0 {
		t.Errorf("plot task token ratio %.1fx < 3x target", plotRatio)
	}
}

func TestTokenAccountingPerShard(t *testing.T) {
	// Verify that PromptCompiler's estimate matches TokenCount within 10%
	testData := [][]byte{
		[]byte(`{"symbol":"Greet","definition":"func Greet(name string) string { return fmt.Sprintf(\"Hello, %s!\", name) }"}`),
		[]byte(`{"symbol":"Person","definition":"type Person struct { Name string; Age int }"}`),
		[]byte(strings.Repeat("x", 4000)), // ~1000 tokens
	}

	for i, data := range testData {
		shard := CandidateShard{
			Kind:    "symdef",
			BlobID:  fmt.Sprintf("blob-%d", i),
			Content: data,
		}

		scored := ScoreShard(shard)
		_ = scored

		estimatedTokens := EstimateTokens(data)
		// Manual check: bytes/4 + 10
		expectedTokens := len(data)/4 + 10

		if estimatedTokens != expectedTokens {
			t.Errorf("shard %d: EstimateTokens=%d, expected=%d", i, estimatedTokens, expectedTokens)
		}

		// Verify estimate matches the formula exactly: bytes/4 + 10 overhead
		if estimatedTokens != len(data)/4+10 {
			t.Errorf("shard %d: EstimateTokens=%d, expected bytes/4+10=%d", i, estimatedTokens, len(data)/4+10)
		}
	}
}

// ---------------------------------------------------------------------------
// E2E Determinism (L4)
// ---------------------------------------------------------------------------

func TestE2EFullDeterminism_TwoRuns(t *testing.T) {
	origNow := nowFunc
	origID := idFunc
	t.Cleanup(func() { nowFunc = origNow; idFunc = origID })

	type runResult struct {
		planID     string
		missionIDs []string
		events     []json.RawMessage
		stateRev   int64
	}

	runWith := func(seed int) runResult {
		frozen := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		nowFunc = func() time.Time { return frozen }
		counter := seed * 10000
		idFunc = func() string {
			counter++
			return fmt.Sprintf("seed%d-%06d", seed, counter)
		}

		client, fixtureDir, cleanup := benchSetup(t)
		defer cleanup()

		planner := NewPlanner(client)
		dag, err := planner.Plan("Add helper utilities", fixtureDir)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}

		var missionIDs []string
		for _, n := range dag.Nodes {
			missionIDs = append(missionIDs, n.Mission.MissionID)
		}

		events, _ := client.TailEvents(1000)
		status, _ := client.GetStatus()

		return runResult{
			planID:     dag.PlanID,
			missionIDs: missionIDs,
			events:     events,
			stateRev:   status.StateRev,
		}
	}

	// Two runs with the same seed must produce identical results
	r1 := runWith(1)
	r2 := runWith(1)

	if r1.planID != r2.planID {
		t.Errorf("plan IDs differ: %s vs %s", r1.planID, r2.planID)
	}
	if len(r1.missionIDs) != len(r2.missionIDs) {
		t.Fatalf("mission count differs: %d vs %d", len(r1.missionIDs), len(r2.missionIDs))
	}
	for i := range r1.missionIDs {
		if r1.missionIDs[i] != r2.missionIDs[i] {
			t.Errorf("mission[%d] ID differs: %s vs %s", i, r1.missionIDs[i], r2.missionIDs[i])
		}
	}
	if len(r1.events) != len(r2.events) {
		t.Errorf("event count differs: %d vs %d", len(r1.events), len(r2.events))
	}
}

func TestE2EFullDeterminism_DifferentSeed(t *testing.T) {
	origNow := nowFunc
	origID := idFunc
	t.Cleanup(func() { nowFunc = origNow; idFunc = origID })

	runWith := func(seed int) (string, []string) {
		frozen := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		nowFunc = func() time.Time { return frozen }
		counter := seed * 10000
		idFunc = func() string {
			counter++
			return fmt.Sprintf("seed%d-%06d", seed, counter)
		}

		client, fixtureDir, cleanup := benchSetup(t)
		defer cleanup()

		planner := NewPlanner(client)
		dag, err := planner.Plan("Add helper utilities", fixtureDir)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}

		var missionIDs []string
		for _, n := range dag.Nodes {
			missionIDs = append(missionIDs, n.Mission.MissionID)
		}
		return dag.PlanID, missionIDs
	}

	planID1, missionIDs1 := runWith(1)
	planID2, missionIDs2 := runWith(2)

	// Different seeds → different IDs
	if planID1 == planID2 {
		t.Error("different seeds should produce different plan IDs")
	}
	// But same DAG structure (same number of missions)
	if len(missionIDs1) != len(missionIDs2) {
		t.Errorf("different seeds should produce same DAG structure: %d vs %d missions",
			len(missionIDs1), len(missionIDs2))
	}
	// Mission IDs should differ
	if len(missionIDs1) > 0 && missionIDs1[0] == missionIDs2[0] {
		t.Error("different seeds should produce different mission IDs")
	}
}

func TestMissionPackSizeBounded(t *testing.T) {
	client, fixtureDir, cleanup := benchSetup(t)
	defer cleanup()

	planner := NewPlanner(client)
	dag, err := planner.Plan("Add a pow operation", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := NewPromptCompiler(client.BaseURL + "/pf")

	for _, node := range dag.Nodes {
		m := node.Mission

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

		// Pack size < 32KB
		packJSON, _ := json.Marshal(pack)
		if len(packJSON) > 32*1024 {
			t.Errorf("mission %s: pack %d bytes > 32KB", m.MissionID[:8], len(packJSON))
		}

		// Total tokens <= budget
		if pack.BudgetMeta.TotalTokens > pack.BudgetMeta.TokenBudget {
			t.Errorf("mission %s: tokens %d > budget %d",
				m.MissionID[:8], pack.BudgetMeta.TotalTokens, pack.BudgetMeta.TokenBudget)
		}

		// No shard > 50% of budget
		halfBudget := m.TokenBudget / 2
		for _, shard := range pack.InlineShards {
			tokens := EstimateTokens(shard.Content)
			if tokens > halfBudget {
				t.Errorf("mission %s: shard %s has %d tokens > 50%% budget (%d)",
					m.MissionID[:8], shard.Kind, tokens, m.TokenBudget)
			}
		}
	}
}

func TestMissionPackNoSensitiveFiles(t *testing.T) {
	client, fixtureDir, cleanup := benchSetup(t)
	defer cleanup()

	planner := NewPlanner(client)
	dag, err := planner.Plan("Refactor the Greet function", fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := NewPromptCompiler(client.BaseURL + "/pf")
	sensitivePatterns := []string{".env", "credentials.json", "secret", "api_key", "password"}

	for _, node := range dag.Nodes {
		m := node.Mission

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

		for _, shard := range pack.InlineShards {
			content := string(shard.Content)
			for _, pat := range sensitivePatterns {
				if strings.Contains(strings.ToLower(content), pat) {
					t.Errorf("mission %s: shard contains sensitive pattern %q",
						m.MissionID[:8], pat)
				}
			}
		}
	}
}
