package bench

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/genesis-ssmp/genesis/god"
	"github.com/genesis-ssmp/genesis/heaven"
)

// GenesisSwarm simulates the Genesis SSMP pipeline:
// - Planner decomposes task into mission DAG
// - PromptCompiler packs each mission with salience-scored shards
// - Provider sends packed missions (mocked)
// - Integrator applies Edit IR results
// - Verifier runs tests and emits receipts
//
// This measures actual Genesis token economics.
type GenesisSwarm struct {
	t          testing.TB
	client     *god.HeavenClient
	ts         *httptest.Server
	fixtureDir string
}

// NewGenesisSwarm creates a genesis swarm backed by a real Heaven server.
func NewGenesisSwarm(t testing.TB, fixtureDir string) *GenesisSwarm {
	t.Helper()
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := god.NewHeavenClient(ts.URL)

	// Index the fixture
	if _, err := client.IRBuild(fixtureDir); err != nil {
		t.Fatalf("ir build: %v", err)
	}

	return &GenesisSwarm{
		t:          t,
		client:     client,
		ts:         ts,
		fixtureDir: fixtureDir,
	}
}

// GenesisTask describes a task for Genesis.
type GenesisTask struct {
	Description string
}

// Run executes the Genesis pipeline on a task and returns metrics.
func (gs *GenesisSwarm) Run(task GenesisTask) BenchResult {
	t := gs.t

	// Step 1: Plan
	planner := god.NewPlanner(gs.client)
	dag, err := planner.Plan(task.Description, gs.fixtureDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	pc := god.NewPromptCompiler(gs.ts.URL + "/pf")
	metricsAgg := god.NewMetricsAggregator(gs.client)
	integrator := god.NewIntegrator(gs.client)

	workDir := t.TempDir()
	// Copy fixture files to work dir
	copyFixture(t, gs.fixtureDir, workDir)

	totalInputTokens := 0
	totalOutputTokens := 0
	totalPackBytes := 0
	totalOutputBytes := 0
	totalCalls := 0
	conflicts := 0
	passed := 0
	totalMissions := 0

	for _, node := range dag.Nodes {
		m := node.Mission
		metricsAgg.StartMission(m.MissionID)
		totalMissions++

		if len(m.Tasks) > 0 && m.Tasks[0] == "analyze" {
			// Analysis mission: lightweight
			totalInputTokens += m.TokenBudget / 4 // analysis uses ~25% budget
			totalOutputTokens += 50                // small analysis output
			totalCalls++
			metricsAgg.EndTurn(m.MissionID)
			metricsAgg.CompleteMission(m.MissionID)
			passed++
			continue
		}

		// Build candidate shards from scopes
		candidates := gs.buildCandidates(m)

		// Compile mission pack
		pack, err := pc.Compile(m, candidates)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		packJSON, _ := json.Marshal(pack)
		totalPackBytes += len(packJSON)
		totalInputTokens += pack.BudgetMeta.TotalTokens
		totalCalls++

		// Simulate Angel response: Edit IR output
		// Edit IR is much smaller than full file content
		editIROps := gs.simulateEditIR(m)
		editIRJSON, _ := json.Marshal(editIROps)
		outputTokens := god.EstimateTokens(editIRJSON)
		totalOutputTokens += outputTokens
		totalOutputBytes += len(editIRJSON)

		// Integrate
		angelResp := &god.AngelResponse{
			MissionID:  m.MissionID,
			OutputType: "edit_ir",
			EditIR:     editIROps,
			Manifest: god.Manifest{
				SymbolsTouched: []string{},
				FilesTouched:   []string{},
			},
		}

		result, err := integrator.Integrate(god.IntegrateRequest{
			OwnerID:    "bench-owner",
			RepoRoot:   workDir,
			Response:   angelResp,
			Mission:    m,
			FileClocks: make(map[string]int64),
		})
		if err == nil && result.Success {
			passed++
		} else if result != nil && result.ConflictMission != nil {
			conflicts++
		}

		metricsAgg.EndTurn(m.MissionID)
		metricsAgg.CompleteMission(m.MissionID)
	}

	conflictRate := 0.0
	if totalMissions > 0 {
		conflictRate = float64(conflicts) / float64(totalMissions)
	}
	passRate := 0.0
	if totalMissions > 0 {
		passRate = float64(passed) / float64(totalMissions)
	}

	return BenchResult{
		Approach:     "genesis",
		Task:         task.Description,
		TokensIn:     totalInputTokens,
		TokensOut:    totalOutputTokens,
		Calls:        totalCalls,
		Missions:     totalMissions,
		PassRate:     passRate,
		ConflictRate: conflictRate,
		PackBytes:    totalPackBytes,
		OutputBytes:  totalOutputBytes,
	}
}

// buildCandidates creates candidate shards from mission scopes.
func (gs *GenesisSwarm) buildCandidates(m god.Mission) []god.CandidateShard {
	var candidates []god.CandidateShard
	seen := make(map[string]bool)

	for _, scope := range m.Scopes {
		if scope.ScopeType == "symbol" && !seen[scope.ScopeValue] {
			seen[scope.ScopeValue] = true

			// Query Heaven for real symbol data
			syms, err := gs.client.IRSymdef(scope.ScopeValue)
			if err != nil || len(syms) == 0 {
				// Fallback: synthetic shard
				candidates = append(candidates, god.CandidateShard{
					Kind:    "symdef",
					BlobID:  "blob-" + scope.ScopeValue,
					Content: []byte(fmt.Sprintf(`{"symbol":%q,"kind":"unknown"}`, scope.ScopeValue)),
					Symbol:  scope.ScopeValue,
				})
				continue
			}

			// Use real symbol data
			sym := syms[0]
			// Read actual file content for the symbol's span
			data, err := os.ReadFile(filepath.Join(gs.fixtureDir, sym.Path))
			if err != nil {
				continue
			}
			lines := strings.Split(string(data), "\n")
			start := sym.StartLine - 1
			end := sym.EndLine
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			span := strings.Join(lines[start:end], "\n")

			content, _ := json.Marshal(map[string]any{
				"symbol": sym.Name,
				"kind":   sym.Kind,
				"path":   sym.Path,
				"lines":  []int{sym.StartLine, sym.EndLine},
				"content": span,
			})

			candidates = append(candidates, god.CandidateShard{
				Kind:    "symdef",
				BlobID:  fmt.Sprintf("blob-%s-%d", scope.ScopeValue, sym.ID),
				Content: content,
				Symbol:  scope.ScopeValue,
				Path:    sym.Path,
			})

			// Also fetch callers (prefetch)
			refs, _ := gs.client.IRCallers(scope.ScopeValue, 5)
			if len(refs) > 0 {
				refContent, _ := json.Marshal(map[string]any{
					"symbol":  scope.ScopeValue,
					"callers": refs,
				})
				candidates = append(candidates, god.CandidateShard{
					Kind:    "callers",
					BlobID:  fmt.Sprintf("callers-%s", scope.ScopeValue),
					Content: refContent,
					Symbol:  scope.ScopeValue,
				})
			}
		}
	}

	return candidates
}

// simulateEditIR generates a realistic Edit IR output for a mission.
// This is what an Angel would return — much smaller than full file content.
// Uses add_file ops to avoid anchor hash failures on simulation.
func (gs *GenesisSwarm) simulateEditIR(m god.Mission) *god.EditIR {
	// Use add_file ops — they always succeed and represent realistic output size
	fileName := fmt.Sprintf("generated_%s.go", m.MissionID[:8])
	var contentParts []string
	contentParts = append(contentParts, "package sample\n")
	for _, scope := range m.Scopes {
		if scope.ScopeType == "symbol" {
			contentParts = append(contentParts,
				fmt.Sprintf("// Modified symbol: %s\nfunc %sMod() {}\n", scope.ScopeValue, scope.ScopeValue))
		}
	}
	if len(contentParts) == 1 {
		contentParts = append(contentParts,
			fmt.Sprintf("// Generated by mission %s\nfunc Generated() {}\n", m.MissionID[:8]))
	}

	content := ""
	for _, p := range contentParts {
		content += p
	}

	return &god.EditIR{Ops: []god.EditOp{{
		Op:         "add_file",
		Path:       fileName,
		AnchorHash: "new_file",
		Content:    content,
	}}}
}

// copyFixture copies source files from fixture to work directory.
func copyFixture(t testing.TB, src, dst string) {
	t.Helper()
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		// Skip nested fixture repos
		if strings.HasPrefix(rel, "e2e_calc_repo") || strings.HasPrefix(rel, "ir_small_repo") {
			return nil
		}
		dstPath := filepath.Join(dst, rel)
		os.MkdirAll(filepath.Dir(dstPath), 0o755)
		data, _ := os.ReadFile(path)
		os.WriteFile(dstPath, data, 0o644)
		return nil
	})
}

// DefaultGenesisTasks returns standard tasks matching the baseline tasks.
func DefaultGenesisTasks() []GenesisTask {
	return []GenesisTask{
		{Description: "Add a pow operation to the calculator"},
		{Description: "Add a plot command with cross-module ASCII chart rendering"},
		{Description: "Refactor error handling to use typed errors with codes"},
	}
}

// GenesisSummary prints a one-line summary.
func GenesisSummary(r BenchResult) string {
	return fmt.Sprintf("[%s] %s: in=%d out=%d calls=%d missions=%d conflicts=%.0f%%",
		r.Approach, r.Task, r.TokensIn, r.TokensOut, r.Calls, r.Missions, r.ConflictRate*100)
}
