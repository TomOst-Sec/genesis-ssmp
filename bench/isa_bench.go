package bench

import (
	"encoding/json"
	"fmt"

	"github.com/genesis-ssmp/genesis/god"
)

// ISABenchScenario describes a benchmark comparing verbose prompt vs ISA-compiled input.
type ISABenchScenario struct {
	Name          string
	VerbosePrompt string // multi-line natural language prompt
	ISASource     string // equivalent ISA program
	TokenBudget   int
}

// ISABenchResult holds the comparison for one ISA benchmark scenario.
type ISABenchResult struct {
	Scenario         string  `json:"scenario"`
	BaselineTokensIn int     `json:"baseline_tokens_in"`
	GenesisTokensIn  int     `json:"genesis_tokens_in"`
	Ratio            float64 `json:"ratio"`
	Pass             bool    `json:"pass"`
}

// ISABenchBaseline simulates a verbose natural-language prompt approach.
// In traditional approaches, the full prompt + relevant file contents are sent each turn.
type ISABenchBaseline struct{}

// Run estimates the input tokens for a verbose prompt baseline.
// Traditional agents paste: system prompt + verbose prompt + full file contents.
func (b *ISABenchBaseline) Run(scenario ISABenchScenario) ISABenchResult {
	promptTokens := EstimateTokens([]byte(scenario.VerbosePrompt))

	// Traditional agent overhead: system prompt + full file contents pasted inline
	systemPrompt := 500  // typical system prompt
	fileContents := 2000 // relevant files pasted in full (the biggest cost)
	missionOverhead := 200
	conversationCtx := 300 // prior turn context

	totalIn := systemPrompt + promptTokens + fileContents + missionOverhead + conversationCtx

	return ISABenchResult{
		Scenario:         scenario.Name,
		BaselineTokensIn: totalIn,
	}
}

// ISABenchGenesis simulates ISA-compiled input.
// The ISA compiles to a compact MissionPack — only the pack is sent to the provider.
type ISABenchGenesis struct{}

// Run parses and compiles the ISA source, then estimates the pack tokens.
// Genesis sends: compact header + compiled mission JSON + shard pointers (not full content).
func (g *ISABenchGenesis) Run(scenario ISABenchScenario) ISABenchResult {
	prog, err := god.ParseISA(scenario.ISASource)
	if err != nil {
		return ISABenchResult{
			Scenario:        scenario.Name,
			GenesisTokensIn: 0,
		}
	}

	compiled, err := god.CompileISA(prog)
	if err != nil {
		return ISABenchResult{
			Scenario:        scenario.Name,
			GenesisTokensIn: 0,
		}
	}

	// Pack header is compact and standard
	headerTokens := 60

	// Mission JSON from compilation (compact — just goal, scopes, budget)
	missionJSON, _ := json.Marshal(compiled.Mission)
	missionTokens := EstimateTokens(missionJSON)

	// Shard pointers (not full file content — just metadata + salience-packed snippets)
	shardTokens := len(compiled.ShardRequests) * 40

	totalIn := headerTokens + missionTokens + shardTokens

	return ISABenchResult{
		Scenario:        scenario.Name,
		GenesisTokensIn: totalIn,
	}
}

// DefaultISABenchScenarios returns ISA benchmark scenarios.
func DefaultISABenchScenarios() []ISABenchScenario {
	return []ISABenchScenario{
		{
			Name: "Simple add function",
			VerbosePrompt: `You are an AI coding assistant working on a Go codebase.

Task: Add a pow(x,y) operation to the calculator.

Instructions:
1. First, look up the definition of the Add function and understand its structure and signature.
2. Find all callers of the Add function to understand the usage pattern and calling conventions.
3. Add a new function Pow that computes x raised to the power y, following the exact same pattern as the existing Add function.
4. Make sure to add appropriate documentation comments.
5. Run all tests to verify nothing is broken and the new function works correctly.
6. If tests fail, retry up to 2 times before reporting failure.

Constraints:
- Do not modify existing tests under any circumstances
- Use Edit IR JSON output format only (no full file dumps, no diffs)
- Each edit operation must include an anchor_hash for verification
- Token budget: 8000 tokens — request only needed context via PF
- Do not guess file contents — use PF_SLICE to read before editing
- Base revision: HEAD

Working set: Add, Multiply, Subtract, Calculator
Test targets: ops/math_test.go, calc_test.go`,
			ISASource: `ISA_VERSION 0
BASE_REV HEAD
MODE SOLO
BUDGET 8000
INVARIANT "Do not modify existing tests"
NEED symdef Add
NEED callers Add
OP Add function Pow computing x^y following Add pattern
RUN test ./...
ASSERT all tests pass
IF_FAIL RETRY 2`,
			TokenBudget: 8000,
		},
		{
			Name: "Multi-file refactor",
			VerbosePrompt: `You are an AI coding assistant working on a Go codebase.

Task: Rename the function Calculate to Compute across the entire codebase.

Instructions:
1. Look up the definition of the Calculate function to understand its full signature and location.
2. Find ALL callers of Calculate across the entire codebase — this is critical for a complete rename.
3. Look up the definition of the Result type since it may be related.
4. Find ALL callers of Result in case the type name needs updating in signatures.
5. For each file containing Calculate:
   a. Read the relevant section using PF_SLICE
   b. Replace all occurrences of Calculate with Compute
   c. Update any documentation comments referencing the old name
6. Run all tests and the linter to verify the rename is complete.
7. If any test fails, retry up to 3 times.
8. If linting fails, fix the issues and retry.

Constraints:
- Do not change function behavior — this is a pure rename operation
- Do not modify test assertions or expected values (unless they reference the old name)
- Use Edit IR JSON output format only
- Each edit operation must include an anchor_hash
- Token budget: 12000 tokens
- Base revision: HEAD

Working set: Calculate, Compute, Result, Handler
This is a multi-file operation requiring careful coordination.`,
			ISASource: `ISA_VERSION 0
BASE_REV HEAD
MODE SOLO
BUDGET 12000
INVARIANT "Do not change function behavior"
INVARIANT "Pure rename operation only"
NEED symdef Calculate
NEED callers Calculate
NEED symdef Result
NEED callers Result
OP Rename Calculate to Compute in all files
RUN test ./...
RUN lint
ASSERT all tests pass
ASSERT lint passes
IF_FAIL RETRY 3`,
			TokenBudget: 12000,
		},
		{
			Name: "Complex feature with tests",
			VerbosePrompt: `You are an AI coding assistant working on a Go codebase.

Task: Add an ASCII plot feature that renders sin(x) over a range with configurable sample count.

Instructions:
1. Understand the existing codebase structure:
   a. Look up the definition of the Add function for the function pattern
   b. Look up all callers of Add to understand how operations are integrated
   c. Look up the definition of the Calculator type/struct
   d. Find all callers of Calculator to understand the integration points
   e. Examine the test file structure for the test pattern
   f. Search for any existing rendering or output formatting code
2. Design the implementation:
   a. Create a PlotSin function that takes range start, end, and sample count
   b. Create an ASCIIRenderer that converts float values to ASCII art
   c. Add a Plot CLI command that wires everything together
3. Implement in this order:
   a. ASCIIRenderer helper
   b. PlotSin core function
   c. CLI integration
   d. Tests for all three components
4. Run all tests including the new ones.
5. Run linter and type checker.
6. If tests fail, retry up to 2 times.
7. If type checking fails, fix type errors.

Constraints:
- Do not modify existing tests
- Follow existing code conventions and patterns
- Use Edit IR JSON output format only
- Each edit operation must include an anchor_hash
- Token budget: 16000 tokens
- Base revision: HEAD
- Output must be well-formed Go code that compiles

Working set: Add, Calculator, main, TestAdd, PlotSin, ASCIIRenderer
Test targets: ops/math_test.go, plot_test.go, render_test.go`,
			ISASource: `ISA_VERSION 0
BASE_REV HEAD
MODE SOLO
BUDGET 16000
INVARIANT "Do not modify existing tests"
INVARIANT "Follow existing code conventions"
NEED symdef Add
NEED callers Add
NEED symdef Calculator
NEED callers Calculator
NEED test TestAdd
OP Add ASCIIRenderer helper for float-to-ascii conversion
OP Add PlotSin function with range and sample count
OP Add Plot CLI command wiring renderer and PlotSin
OP Add tests for ASCIIRenderer, PlotSin, and Plot CLI
RUN test ./...
RUN lint
RUN typecheck
ASSERT all tests pass
ASSERT lint passes
ASSERT typecheck passes
IF_FAIL RETRY 2`,
			TokenBudget: 16000,
		},
	}
}

// PrintISABenchReport prints a comparison table for ISA benchmarks.
func PrintISABenchReport(results []ISABenchResult) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                       ISA BENCH REPORT                                  ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Scenario                          Baseline     Genesis      Ratio  PASS ║")
	fmt.Println("║  ──────────────────────────────────────────────────────────────────────   ║")

	for _, r := range results {
		pass := "FAIL"
		if r.Pass {
			pass = "PASS"
		}
		fmt.Printf("║  %-36s %-12d %-12d %-5.1fx %s ║\n",
			r.Scenario, r.BaselineTokensIn, r.GenesisTokensIn, r.Ratio, pass)
	}

	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}
