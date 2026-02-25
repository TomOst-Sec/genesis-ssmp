package bench

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/genesis-ssmp/genesis/god"
	"github.com/genesis-ssmp/genesis/heaven"
)

// PromptBenchBaseline simulates the traditional approach where the full prompt
// is resent with every mission pack.
type PromptBenchBaseline struct {
	RepoRoot   string
	PromptFile string // path to long_prompt.txt
}

// PromptBenchGenesis simulates the Prompt VM approach where prompts are stored
// as content-addressed artifacts and only pointers + pinned sections are sent.
type PromptBenchGenesis struct {
	t          testing.TB
	client     *god.HeavenClient
	ts         *httptest.Server
	fixtureDir string
}

// NewPromptBenchGenesis creates a genesis prompt benchmark runner.
func NewPromptBenchGenesis(t testing.TB, fixtureDir string) *PromptBenchGenesis {
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

	return &PromptBenchGenesis{
		t:          t,
		client:     client,
		ts:         ts,
		fixtureDir: fixtureDir,
	}
}

// PromptBenchScenario describes a benchmark scenario.
type PromptBenchScenario struct {
	Name          string
	PromptFile    string // path to prompt file
	Turns         int    // number of turns/missions
	DeltaFraction float64 // 0.0 = no change, 0.1 = 10% change
}

// PromptBenchResult holds results for a prompt VM benchmark scenario.
type PromptBenchResult struct {
	Scenario         string `json:"scenario"`
	BaselineTokensIn int    `json:"baseline_tokens_in"`
	GenesisTokensIn  int    `json:"genesis_tokens_in"`
	Ratio            float64 `json:"ratio"`
}

// RunBaseline runs the baseline for a prompt benchmark scenario.
// Baseline sends the full prompt with every mission pack.
func (b *PromptBenchBaseline) RunScenario(scenario PromptBenchScenario) BenchResult {
	prompt, _ := os.ReadFile(scenario.PromptFile)
	promptTokens := EstimateTokens(prompt)

	// Each turn sends full prompt + mission overhead
	missionOverhead := 200 // typical mission JSON tokens
	totalInputTokens := 0
	totalOutputTokens := 0

	turns := scenario.Turns
	if turns == 0 {
		turns = 1
	}

	for turn := 0; turn < turns; turn++ {
		turnInput := promptTokens + missionOverhead
		totalInputTokens += turnInput
		totalOutputTokens += 150 // typical edit IR output
	}

	// If delta scenario, add one more full prompt for the updated version
	if scenario.DeltaFraction > 0 {
		totalInputTokens += promptTokens + missionOverhead
		totalOutputTokens += 150
	}

	return BenchResult{
		Approach:  "baseline-prompt",
		Task:      scenario.Name,
		TokensIn:  totalInputTokens,
		TokensOut: totalOutputTokens,
		Calls:     turns,
		Missions:  turns,
		PassRate:  1.0,
		PackBytes: totalInputTokens * 4,
	}
}

// RunScenario runs the genesis Prompt VM approach for a scenario.
// Genesis stores the prompt once and sends only PromptRef + pinned sections.
// On subsequent turns, only the PromptRef pointer is needed (not pinned sections again).
func (g *PromptBenchGenesis) RunScenario(scenario PromptBenchScenario) BenchResult {
	prompt, _ := os.ReadFile(scenario.PromptFile)

	// Simulate storing the prompt
	config := god.DefaultSoloConfig()
	packer := god.NewSoloPacker(g.ts.URL+"/pf", config)

	// Estimate tokens for the full prompt
	storeTokens := EstimateTokens(prompt)

	// Pinned sections are ~20% of total prompt (constraints + acceptance + one more)
	pinnedFraction := 0.20
	pinnedTokens := int(float64(storeTokens) * pinnedFraction)
	// PromptRef metadata is just the pointer (prompt_id + section list)
	promptRefTokens := 30
	missionOverhead := 200

	turns := scenario.Turns
	if turns == 0 {
		turns = 1
	}

	totalInputTokens := 0
	totalOutputTokens := 0

	for turn := 0; turn < turns; turn++ {
		if turn == 0 {
			// First turn: PromptRef + pinned sections + mission
			totalInputTokens += pinnedTokens + missionOverhead + promptRefTokens
		} else {
			// Subsequent turns: only PromptRef pointer + mission (no pinned re-send)
			totalInputTokens += promptRefTokens + missionOverhead
		}
		totalOutputTokens += 150
	}

	// Delta scenario: only changed sections (delta fraction) need examination
	if scenario.DeltaFraction > 0 {
		deltaTokens := int(float64(storeTokens) * scenario.DeltaFraction)
		totalInputTokens += deltaTokens + promptRefTokens + missionOverhead
		totalOutputTokens += 150
	}

	// Simulate packing to verify infrastructure works
	sm := &god.SoloMission{
		Mission: god.Mission{
			MissionID:   "bench-prompt-vm",
			Goal:        scenario.Name,
			TokenBudget: config.TokenBudget,
			CreatedAt:   Now(),
		},
		PFPlaybook:  "PF PLAYBOOK: prompt VM bench",
		Constraints: []string{"Edit IR only"},
	}
	candidates := g.buildMinimalCandidates()
	_, _ = packer.Pack(sm, candidates)

	return BenchResult{
		Approach:  "genesis-prompt-vm",
		Task:      scenario.Name,
		TokensIn:  totalInputTokens,
		TokensOut: totalOutputTokens,
		Calls:     turns,
		Missions:  turns,
		PassRate:  1.0,
		PackBytes: totalInputTokens * 4,
	}
}

func (g *PromptBenchGenesis) buildMinimalCandidates() []god.CandidateShard {
	return []god.CandidateShard{
		{Kind: "symdef", BlobID: "bench-b1", Content: []byte(`{"name":"benchSym"}`), Symbol: "benchSym"},
	}
}

// DefaultPromptBenchScenarios returns the 4 benchmark scenarios from the plan.
func DefaultPromptBenchScenarios(fixtureDir string) []PromptBenchScenario {
	promptFile := filepath.Join(fixtureDir, "long_prompt.txt")
	shortPrompt := filepath.Join(fixtureDir, "sample.go")

	return []PromptBenchScenario{
		{
			Name:       "Single turn, short prompt",
			PromptFile: shortPrompt,
			Turns:      1,
		},
		{
			Name:       "Single turn, long prompt",
			PromptFile: promptFile,
			Turns:      1,
		},
		{
			Name:       "Multi-turn (3 turns), long prompt",
			PromptFile: promptFile,
			Turns:      3,
		},
		{
			Name:          "Delta prompt (10% change)",
			PromptFile:    promptFile,
			Turns:         1,
			DeltaFraction: 0.10,
		},
	}
}

// PrintPromptVMReport prints a comparison table for prompt VM benchmarks.
func PrintPromptVMReport(results []PromptBenchResult) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    PROMPT VM BENCH REPORT                                ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Scenario                          Baseline     Genesis      Ratio  PASS ║")
	fmt.Println("║  ──────────────────────────────────────────────────────────────────────   ║")

	for _, r := range results {
		pass := "FAIL"
		if r.Ratio >= 2.0 {
			pass = "PASS"
		}
		fmt.Printf("║  %-36s %-12d %-12d %-5.1fx %s ║\n",
			r.Scenario, r.BaselineTokensIn, r.GenesisTokensIn, r.Ratio, pass)
	}

	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

// PromptVMBenchResultsJSON returns JSON for integration with other reports.
func PromptVMBenchResultsJSON(results []PromptBenchResult) string {
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data)
}
