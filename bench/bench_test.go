package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.Abs("../fixtures")
	if _, err := os.Stat(filepath.Join(dir, "sample.go")); os.IsNotExist(err) {
		t.Skipf("fixtures not found at %s", dir)
	}
	return dir
}

func calcRepoDir(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.Abs("../fixtures/e2e_calc_repo")
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); os.IsNotExist(err) {
		t.Skipf("e2e_calc_repo not found at %s", dir)
	}
	return dir
}

// TestBenchReport runs both baseline and genesis on fixture tasks and prints
// a comparison table. This is the core deliverable.
func TestBenchReport(t *testing.T) {
	fixture := fixtureDir(t)
	calcRepo := calcRepoDir(t)

	// --- Baseline ---
	baselineTasks := DefaultBaselineTasks(calcRepo)
	baselineResults := BaselineRunOnFixture(calcRepo, baselineTasks)

	for _, r := range baselineResults {
		t.Log(BaselineSummary(r))
	}

	// --- Genesis ---
	gs := NewGenesisSwarm(t, fixture)
	genesisTasks := DefaultGenesisTasks()
	var genesisResults []BenchResult
	for _, task := range genesisTasks {
		r := gs.Run(task)
		genesisResults = append(genesisResults, r)
		t.Log(GenesisSummary(r))
	}

	// --- Build report ---
	report := BenchReport{
		Timestamp: Now(),
		Fixture:   "fixtures + e2e_calc_repo",
	}

	for i := range baselineResults {
		if i >= len(genesisResults) {
			break
		}
		report.Tasks = append(report.Tasks, TaskReport{
			Task:     baselineResults[i].Task,
			Baseline: baselineResults[i],
			Genesis:  genesisResults[i],
		})
	}

	PrintReport(report)

	// --- Assert targets ---
	for _, tr := range report.Tasks {
		ratio := safeDiv(tr.Baseline.TokensIn, tr.Genesis.TokensIn)
		if ratio < 3.0 {
			t.Errorf("task %q: tokens_in ratio %.1fx < 3x minimum", tr.Task, ratio)
		}
		t.Logf("task %q: tokens_in ratio = %.1fx", tr.Task, ratio)
	}

	// At least one task must hit 5x
	maxRatio := 0.0
	for _, tr := range report.Tasks {
		ratio := safeDiv(tr.Baseline.TokensIn, tr.Genesis.TokensIn)
		if ratio > maxRatio {
			maxRatio = ratio
		}
	}
	if maxRatio < 5.0 {
		t.Errorf("no task reached 5x tokens_in ratio (max was %.1fx)", maxRatio)
	}
}

// ---------------------------------------------------------------------------
// Single-Agent Benchmark: baseline single-agent vs genesis single-agent
// ---------------------------------------------------------------------------

func TestSingleAgentBenchReport(t *testing.T) {
	fixture := fixtureDir(t)
	calcRepo := calcRepoDir(t)

	// --- Baseline single agent ---
	baselineTasks := DefaultSingleAgentBaselineTasks(calcRepo)
	sab := &SingleAgentBaseline{RepoRoot: calcRepo}
	var baselineResults []BenchResult
	for _, task := range baselineTasks {
		r := sab.Run(task)
		baselineResults = append(baselineResults, r)
		t.Log(SingleAgentBaselineSummary(r))
	}

	// --- Genesis single agent ---
	sag := NewSingleAgentGenesis(t, fixture)
	genesisTasks := DefaultSingleAgentGenesisTasks()
	var genesisResults []BenchResult
	for _, task := range genesisTasks {
		r := sag.Run(task)
		genesisResults = append(genesisResults, r)
		t.Log(SingleAgentGenesisSummary(r))
	}

	// --- Build report ---
	report := BenchReport{
		Timestamp: Now(),
		Fixture:   "single-agent: e2e_calc_repo",
	}
	for i := range baselineResults {
		if i >= len(genesisResults) {
			break
		}
		report.Tasks = append(report.Tasks, TaskReport{
			Task:     baselineResults[i].Task,
			Baseline: baselineResults[i],
			Genesis:  genesisResults[i],
		})
	}

	PrintSingleAgentReport(report)

	// --- Assert targets ---
	// Task A: total_ratio >= 5x
	if len(report.Tasks) > 0 {
		tr := report.Tasks[0]
		bTotal := tr.Baseline.TokensIn + tr.Baseline.TokensOut
		gTotal := tr.Genesis.TokensIn + tr.Genesis.TokensOut
		totalRatio := safeDiv(bTotal, gTotal)
		t.Logf("Task A %q: total_ratio = %.1fx (in=%.1fx out=%.1fx)",
			tr.Task,
			totalRatio,
			safeDiv(tr.Baseline.TokensIn, tr.Genesis.TokensIn),
			safeDiv(tr.Baseline.TokensOut, tr.Genesis.TokensOut))
		if totalRatio < 5.0 {
			t.Errorf("Task A total_ratio %.1fx < 5x", totalRatio)
		}
		// Call count check: genesis should not exceed baseline by >25%
		if tr.Genesis.Calls > tr.Baseline.Calls*5/4 {
			t.Errorf("Task A genesis calls %d > 125%% of baseline %d", tr.Genesis.Calls, tr.Baseline.Calls)
		}
	}

	// Task B: total_ratio >= 3x (goal 5x)
	if len(report.Tasks) > 1 {
		tr := report.Tasks[1]
		bTotal := tr.Baseline.TokensIn + tr.Baseline.TokensOut
		gTotal := tr.Genesis.TokensIn + tr.Genesis.TokensOut
		totalRatio := safeDiv(bTotal, gTotal)
		t.Logf("Task B %q: total_ratio = %.1fx (in=%.1fx out=%.1fx)",
			tr.Task,
			totalRatio,
			safeDiv(tr.Baseline.TokensIn, tr.Genesis.TokensIn),
			safeDiv(tr.Baseline.TokensOut, tr.Genesis.TokensOut))
		if totalRatio < 3.0 {
			t.Errorf("Task B total_ratio %.1fx < 3x", totalRatio)
		}
	}
}

// ---------------------------------------------------------------------------
// Prompt VM Benchmark: token savings from content-addressed prompt storage
// ---------------------------------------------------------------------------

func TestPromptVMBenchReport(t *testing.T) {
	fixture := fixtureDir(t)

	scenarios := DefaultPromptBenchScenarios(fixture)

	baseline := &PromptBenchBaseline{}
	genesis := NewPromptBenchGenesis(t, fixture)

	var results []PromptBenchResult
	for _, s := range scenarios {
		bResult := baseline.RunScenario(s)
		gResult := genesis.RunScenario(s)

		ratio := 0.0
		if gResult.TokensIn > 0 {
			ratio = float64(bResult.TokensIn) / float64(gResult.TokensIn)
		}

		results = append(results, PromptBenchResult{
			Scenario:         s.Name,
			BaselineTokensIn: bResult.TokensIn,
			GenesisTokensIn:  gResult.TokensIn,
			Ratio:            ratio,
		})

		t.Logf("scenario %q: baseline=%d genesis=%d ratio=%.1fx",
			s.Name, bResult.TokensIn, gResult.TokensIn, ratio)
	}

	PrintPromptVMReport(results)

	// Assert: multi-turn long prompt >= 5x
	for _, r := range results {
		if r.Scenario == "Multi-turn (3 turns), long prompt" {
			if r.Ratio < 5.0 {
				t.Errorf("multi-turn long prompt ratio %.1fx < 5x", r.Ratio)
			}
		}
	}

	// Assert: single turn long prompt >= 3x
	for _, r := range results {
		if r.Scenario == "Single turn, long prompt" {
			if r.Ratio < 3.0 {
				t.Errorf("single turn long prompt ratio %.1fx < 3x", r.Ratio)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ISA Benchmark
// ---------------------------------------------------------------------------

func TestISABenchReport(t *testing.T) {
	scenarios := DefaultISABenchScenarios()
	baseline := &ISABenchBaseline{}
	genesis := &ISABenchGenesis{}

	var results []ISABenchResult
	for _, s := range scenarios {
		bResult := baseline.Run(s)
		gResult := genesis.Run(s)

		ratio := 0.0
		if gResult.GenesisTokensIn > 0 {
			ratio = float64(bResult.BaselineTokensIn) / float64(gResult.GenesisTokensIn)
		}

		results = append(results, ISABenchResult{
			Scenario:         s.Name,
			BaselineTokensIn: bResult.BaselineTokensIn,
			GenesisTokensIn:  gResult.GenesisTokensIn,
			Ratio:            ratio,
			Pass:             ratio >= 3.0,
		})

		t.Logf("ISA scenario %q: baseline=%d genesis=%d ratio=%.1fx",
			s.Name, bResult.BaselineTokensIn, gResult.GenesisTokensIn, ratio)
	}

	PrintISABenchReport(results)

	// Assert: all scenarios >= 3x
	for _, r := range results {
		if r.Ratio < 3.0 {
			t.Errorf("ISA scenario %q: ratio %.1fx < 3x minimum", r.Scenario, r.Ratio)
		}
	}

	// Assert: at least one scenario >= 5x
	maxRatio := 0.0
	for _, r := range results {
		if r.Ratio > maxRatio {
			maxRatio = r.Ratio
		}
	}
	if maxRatio < 5.0 {
		t.Errorf("no ISA scenario reached 5x (max was %.1fx)", maxRatio)
	}
}

// ---------------------------------------------------------------------------
// Macro Ops Benchmark
// ---------------------------------------------------------------------------

func TestMacroOpBenchReport(t *testing.T) {
	scenarios := DefaultMacroOpBenchScenarios()
	baseline := &MacroOpBenchBaseline{}
	genesis := &MacroOpBenchGenesis{}

	var results []MacroOpBenchResult
	for _, s := range scenarios {
		bResult := baseline.Run(s)
		gResult := genesis.Run(s)

		ratio := 0.0
		if gResult.GenesisTokensOut > 0 {
			ratio = float64(bResult.BaselineTokensOut) / float64(gResult.GenesisTokensOut)
		}

		results = append(results, MacroOpBenchResult{
			Scenario:          s.Name,
			BaselineTokensOut: bResult.BaselineTokensOut,
			GenesisTokensOut:  gResult.GenesisTokensOut,
			BaselineOps:       bResult.BaselineOps,
			GenesisOps:        gResult.GenesisOps,
			Ratio:             ratio,
			Pass:              ratio >= 3.0,
		})

		t.Logf("MacroOp scenario %q: baseline=%d/%d genesis=%d/%d ratio=%.1fx",
			s.Name,
			bResult.BaselineOps, bResult.BaselineTokensOut,
			gResult.GenesisOps, gResult.GenesisTokensOut,
			ratio)
	}

	PrintMacroOpBenchReport(results)

	// Assert: all scenarios >= 3x output ratio
	for _, r := range results {
		if r.Ratio < 3.0 {
			t.Errorf("MacroOp scenario %q: ratio %.1fx < 3x minimum", r.Scenario, r.Ratio)
		}
	}
}

// ---------------------------------------------------------------------------
// Combined Pipeline Benchmark
// ---------------------------------------------------------------------------

func TestCombinedBenchReport(t *testing.T) {
	// Run ISA bench
	isaScenarios := DefaultISABenchScenarios()
	isaBaseline := &ISABenchBaseline{}
	isaGenesis := &ISABenchGenesis{}
	var isaResults []ISABenchResult
	for _, s := range isaScenarios {
		bResult := isaBaseline.Run(s)
		gResult := isaGenesis.Run(s)
		ratio := 0.0
		if gResult.GenesisTokensIn > 0 {
			ratio = float64(bResult.BaselineTokensIn) / float64(gResult.GenesisTokensIn)
		}
		isaResults = append(isaResults, ISABenchResult{
			Scenario:         s.Name,
			BaselineTokensIn: bResult.BaselineTokensIn,
			GenesisTokensIn:  gResult.GenesisTokensIn,
			Ratio:            ratio,
			Pass:             ratio >= 3.0,
		})
	}

	// Run Macro Ops bench
	macroScenarios := DefaultMacroOpBenchScenarios()
	macroBaseline := &MacroOpBenchBaseline{}
	macroGenesis := &MacroOpBenchGenesis{}
	var macroResults []MacroOpBenchResult
	for _, s := range macroScenarios {
		bResult := macroBaseline.Run(s)
		gResult := macroGenesis.Run(s)
		ratio := 0.0
		if gResult.GenesisTokensOut > 0 {
			ratio = float64(bResult.BaselineTokensOut) / float64(gResult.GenesisTokensOut)
		}
		macroResults = append(macroResults, MacroOpBenchResult{
			Scenario:          s.Name,
			BaselineTokensOut: bResult.BaselineTokensOut,
			GenesisTokensOut:  gResult.GenesisTokensOut,
			BaselineOps:       bResult.BaselineOps,
			GenesisOps:        gResult.GenesisOps,
			Ratio:             ratio,
			Pass:              ratio >= 3.0,
		})
	}

	// Combined
	combined := RunCombinedBench(isaResults, macroResults)
	PrintCombinedBenchReport(combined)

	t.Logf("Combined: baseline=%d genesis=%d total_ratio=%.1fx isa=%.1fx macro=%.1fx",
		combined.BaselineTotalTokens, combined.GenesisTotalTokens,
		combined.TotalRatio, combined.ISAInputRatio, combined.MacroOpsOutputRatio)

	// Assert: combined >= 5x
	if combined.TotalRatio < 5.0 {
		t.Errorf("combined total_ratio %.1fx < 5x target", combined.TotalRatio)
	}

	// Write CI report
	ciReport := CIReport{
		Timestamp: Now(),
		ISA:       isaResults,
		MacroOps:  macroResults,
		Combined:  &combined,
		Pass:      combined.Pass,
	}

	reportPath := t.TempDir() + "/ci_report.json"
	if err := WriteJSONReport(ciReport, reportPath); err != nil {
		t.Fatalf("write CI report: %v", err)
	}
	t.Logf("CI report written to %s", reportPath)
}

// TestSingleAgentBaseline validates baseline produces reasonable numbers.
func TestSingleAgentBaseline(t *testing.T) {
	calcRepo := calcRepoDir(t)
	tasks := DefaultSingleAgentBaselineTasks(calcRepo)
	sab := &SingleAgentBaseline{RepoRoot: calcRepo}
	for _, task := range tasks {
		r := sab.Run(task)
		t.Log(SingleAgentBaselineSummary(r))
		if r.TokensIn == 0 {
			t.Errorf("task %q: zero input tokens", r.Task)
		}
		if r.Calls != task.Turns {
			t.Errorf("task %q: calls=%d, want %d", r.Task, r.Calls, task.Turns)
		}
	}
}

// TestSingleAgentGenesis validates genesis single-agent produces reasonable numbers.
func TestSingleAgentGenesis(t *testing.T) {
	fixture := fixtureDir(t)
	sag := NewSingleAgentGenesis(t, fixture)
	tasks := DefaultSingleAgentGenesisTasks()
	for _, task := range tasks {
		r := sag.Run(task)
		t.Log(SingleAgentGenesisSummary(r))
		if r.TokensIn == 0 {
			t.Errorf("task %q: zero input tokens", task.Description)
		}
		if r.Calls == 0 {
			t.Errorf("task %q: zero calls", task.Description)
		}
		if r.Missions != 1 {
			t.Errorf("task %q: missions=%d, want 1 (solo mode)", task.Description, r.Missions)
		}
	}
}

// TestLeanBenchReport runs TSLN vs JSON benchmarks across scenarios.
func TestLeanBenchReport(t *testing.T) {
	scenarios := DefaultLeanScenarios()
	var results []LeanBenchResult

	for _, s := range scenarios {
		r := RunLeanBench(s)
		results = append(results, r)
		t.Logf("TSLN %-22s: json=%d tsln=%d ratio=%.1f%% reduction=%.1f%% pass=%v",
			s.Name, r.JSONTokens, r.TSLNTokens, r.Ratio*100, r.Reduction, r.Pass)
	}

	PrintLeanBenchReport(results)

	// Gate: multi-row scenarios must show significant savings
	for _, r := range results {
		if !r.Pass {
			t.Errorf("scenario %q FAILED: ratio %.1f%% > threshold %.1f%%",
				r.Scenario, r.Ratio*100, r.Threshold*100)
		}
	}
}

// TestBaselineSwarm validates baseline simulation produces reasonable numbers.
func TestBaselineSwarm(t *testing.T) {
	calcRepo := calcRepoDir(t)
	tasks := DefaultBaselineTasks(calcRepo)
	results := BaselineRunOnFixture(calcRepo, tasks)

	for _, r := range results {
		t.Log(BaselineSummary(r))
		if r.TokensIn == 0 {
			t.Errorf("task %q: zero input tokens", r.Task)
		}
		if r.TokensOut == 0 {
			t.Errorf("task %q: zero output tokens", r.Task)
		}
		if r.Calls == 0 {
			t.Errorf("task %q: zero calls", r.Task)
		}
		// Baseline should have more tokens than genesis threshold
		if r.TokensIn < 1000 {
			t.Errorf("task %q: suspiciously low input tokens %d", r.Task, r.TokensIn)
		}
	}
}

// TestGenesisSwarm validates genesis simulation produces reasonable numbers.
func TestGenesisSwarm(t *testing.T) {
	fixture := fixtureDir(t)
	gs := NewGenesisSwarm(t, fixture)
	tasks := DefaultGenesisTasks()

	for _, task := range tasks {
		r := gs.Run(task)
		t.Log(GenesisSummary(r))
		if r.TokensIn == 0 {
			t.Errorf("task %q: zero input tokens", task.Description)
		}
		if r.Calls == 0 {
			t.Errorf("task %q: zero calls", task.Description)
		}
		if r.Missions == 0 {
			t.Errorf("task %q: zero missions", task.Description)
		}
		// Genesis pass rate should be high
		if r.PassRate < 0.5 {
			t.Errorf("task %q: low pass rate %.0f%%", task.Description, r.PassRate*100)
		}
	}
}
