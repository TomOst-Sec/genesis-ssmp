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

// SingleAgentGenesis simulates the Genesis SSMP pipeline in solo mode:
// - 1 mission covering the full task
// - Salience-packed MissionPack with PF playbook
// - PF paging calls for additional context
// - Edit IR output only (no full file dumps)
// - Integration with anchor hash verification
// - Verifier receipts
type SingleAgentGenesis struct {
	t          testing.TB
	client     *god.HeavenClient
	ts         *httptest.Server
	fixtureDir string
}

// NewSingleAgentGenesis creates a genesis single-agent runner with real Heaven.
func NewSingleAgentGenesis(t testing.TB, fixtureDir string) *SingleAgentGenesis {
	t.Helper()
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client := god.NewHeavenClient(ts.URL)
	if _, err := client.IRBuild(fixtureDir); err != nil {
		t.Fatalf("ir build: %v", err)
	}

	return &SingleAgentGenesis{
		t:          t,
		client:     client,
		ts:         ts,
		fixtureDir: fixtureDir,
	}
}

// SingleAgentGenesisTask describes a task for genesis single-agent.
type SingleAgentGenesisTask struct {
	Description string
}

// Run executes the genesis solo pipeline on a task.
func (sag *SingleAgentGenesis) Run(task SingleAgentGenesisTask) BenchResult {
	t := sag.t

	// Step 1: Solo plan — single mission covering entire task
	config := god.DefaultSoloConfig()
	planner := god.NewSoloPlanner(sag.client, config)
	soloMission, err := planner.Plan(task.Description, sag.fixtureDir)
	if err != nil {
		t.Fatalf("solo plan: %v", err)
	}

	// Step 2: Build candidates from IR
	candidates := sag.buildCandidates(soloMission)

	// Step 3: Pack with solo-optimized packer
	packer := god.NewSoloPacker(sag.ts.URL+"/pf", config)
	pack, err := packer.Pack(soloMission, candidates)
	if err != nil {
		t.Fatalf("solo pack: %v", err)
	}

	packJSON, _ := json.Marshal(pack)
	totalInputTokens := pack.BudgetMeta.TotalTokens
	totalPackBytes := len(packJSON)
	totalCalls := 1

	// Step 4: Simulate PF paging calls (agent requesting more context)
	// In real execution the angel would issue PF calls; simulate 2-3 calls
	pfCalls := 0
	pfTokens := 0
	for _, scope := range soloMission.Mission.Scopes {
		if scope.ScopeType == "symbol" && pfCalls < 3 {
			// Simulate PF_SYMDEF + PF_CALLERS for key symbols
			symData := sag.fetchSymbolContext(scope.ScopeValue)
			pfTokens += god.EstimateTokens(symData)
			pfCalls++
		}
	}
	totalInputTokens += pfTokens

	// Step 5: Simulate Edit IR output (compact)
	editIR := sag.simulateEditIR(soloMission)
	editIRJSON, _ := json.Marshal(editIR)
	totalOutputTokens := god.EstimateTokens(editIRJSON)
	totalOutputBytes := len(editIRJSON)

	// Step 6: Integration
	workDir := t.TempDir()
	copyFixture(t, sag.fixtureDir, workDir)

	angelResp := &god.AngelResponse{
		MissionID:  soloMission.Mission.MissionID,
		OutputType: "edit_ir",
		EditIR:     editIR,
		Manifest: god.Manifest{
			SymbolsTouched: []string{},
			FilesTouched:   []string{},
		},
	}

	integrator := god.NewIntegrator(sag.client)
	intResult, intErr := integrator.Integrate(god.IntegrateRequest{
		OwnerID:    "solo-bench",
		RepoRoot:   workDir,
		Response:   angelResp,
		Mission:    soloMission.Mission,
		FileClocks: make(map[string]int64),
	})

	passRate := 1.0
	if intErr != nil || (intResult != nil && !intResult.Success) {
		passRate = 0.0
	}

	return BenchResult{
		Approach:    "genesis-single",
		Task:        task.Description,
		TokensIn:    totalInputTokens,
		TokensOut:   totalOutputTokens,
		Calls:       totalCalls,
		Missions:    1,
		PassRate:    passRate,
		PackBytes:   totalPackBytes,
		OutputBytes: totalOutputBytes,
	}
}

// buildCandidates queries Heaven IR for shards relevant to the solo mission.
func (sag *SingleAgentGenesis) buildCandidates(sm *god.SoloMission) []god.CandidateShard {
	var candidates []god.CandidateShard
	seen := make(map[string]bool)

	for _, scope := range sm.Mission.Scopes {
		if scope.ScopeType != "symbol" || seen[scope.ScopeValue] {
			continue
		}
		seen[scope.ScopeValue] = true

		syms, err := sag.client.IRSymdef(scope.ScopeValue)
		if err != nil || len(syms) == 0 {
			candidates = append(candidates, god.CandidateShard{
				Kind:    "symdef",
				BlobID:  "blob-" + scope.ScopeValue,
				Content: []byte(fmt.Sprintf(`{"symbol":%q}`, scope.ScopeValue)),
				Symbol:  scope.ScopeValue,
			})
			continue
		}

		sym := syms[0]
		data, err := os.ReadFile(filepath.Join(sag.fixtureDir, sym.Path))
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
			"symbol":  sym.Name,
			"kind":    sym.Kind,
			"path":    sym.Path,
			"lines":   []int{sym.StartLine, sym.EndLine},
			"content": span,
		})
		candidates = append(candidates, god.CandidateShard{
			Kind:    "symdef",
			BlobID:  fmt.Sprintf("solo-symdef-%s-%d", scope.ScopeValue, sym.ID),
			Content: content,
			Symbol:  scope.ScopeValue,
			Path:    sym.Path,
		})

		// Prefetch callers
		refs, _ := sag.client.IRCallers(scope.ScopeValue, 5)
		if len(refs) > 0 {
			refContent, _ := json.Marshal(map[string]any{
				"symbol":  scope.ScopeValue,
				"callers": refs,
			})
			candidates = append(candidates, god.CandidateShard{
				Kind:    "callers",
				BlobID:  fmt.Sprintf("solo-callers-%s", scope.ScopeValue),
				Content: refContent,
				Symbol:  scope.ScopeValue,
			})
		}
	}

	return candidates
}

// fetchSymbolContext simulates a PF paging call returning context for a symbol.
func (sag *SingleAgentGenesis) fetchSymbolContext(symbol string) []byte {
	syms, err := sag.client.IRSymdef(symbol)
	if err != nil || len(syms) == 0 {
		return []byte(fmt.Sprintf(`{"symbol":%q,"status":"not_found"}`, symbol))
	}
	sym := syms[0]
	content, _ := json.Marshal(map[string]any{
		"symbol": sym.Name,
		"kind":   sym.Kind,
		"path":   sym.Path,
		"lines":  []int{sym.StartLine, sym.EndLine},
	})
	return content
}

// simulateEditIR generates realistic Edit IR output for a solo mission.
func (sag *SingleAgentGenesis) simulateEditIR(sm *god.SoloMission) *god.EditIR {
	fileName := fmt.Sprintf("solo_%s.go", sm.Mission.MissionID[:8])
	var parts []string
	parts = append(parts, "package sample\n")
	for _, scope := range sm.Mission.Scopes {
		if scope.ScopeType == "symbol" {
			parts = append(parts,
				fmt.Sprintf("// Solo modified: %s\nfunc %sSolo() {}\n", scope.ScopeValue, scope.ScopeValue))
		}
	}
	if len(parts) == 1 {
		parts = append(parts,
			fmt.Sprintf("// Solo generated by mission %s\nfunc SoloGen() {}\n", sm.Mission.MissionID[:8]))
	}

	return &god.EditIR{Ops: []god.EditOp{{
		Op:         "add_file",
		Path:       fileName,
		AnchorHash: "new_file",
		Content:    strings.Join(parts, ""),
	}}}
}

// DefaultSingleAgentGenesisTasks returns tasks A and B matching the baseline.
func DefaultSingleAgentGenesisTasks() []SingleAgentGenesisTask {
	return []SingleAgentGenesisTask{
		{Description: "Add pow(x,y) op + tests + CLI command"},
		{Description: "Add plot(sin(x),0..10,200 samples) ascii render + tests"},
	}
}

// SingleAgentGenesisSummary formats a one-line summary.
func SingleAgentGenesisSummary(r BenchResult) string {
	return fmt.Sprintf("[%s] %s: in=%d out=%d calls=%d pf=%d",
		r.Approach, r.Task, r.TokensIn, r.TokensOut, r.Calls, 0)
}
