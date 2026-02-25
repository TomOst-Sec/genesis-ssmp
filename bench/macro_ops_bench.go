package bench

import (
	"encoding/json"
	"fmt"

	"github.com/genesis-ssmp/genesis/god"
)

// MacroOpBenchScenario describes a benchmark comparing verbose Edit IR vs Macro Ops output.
type MacroOpBenchScenario struct {
	Name          string
	VerboseEditIR []god.EditOp  // N individual replace_span ops
	MacroOps      []god.MacroOp // equivalent compact macro ops
}

// MacroOpBenchResult holds the comparison for one macro ops benchmark scenario.
type MacroOpBenchResult struct {
	Scenario          string  `json:"scenario"`
	BaselineTokensOut int     `json:"baseline_tokens_out"`
	GenesisTokensOut  int     `json:"genesis_tokens_out"`
	BaselineOps       int     `json:"baseline_ops"`
	GenesisOps        int     `json:"genesis_ops"`
	Ratio             float64 `json:"ratio"`
	Pass              bool    `json:"pass"`
}

// MacroOpBenchBaseline estimates tokens for verbose Edit IR output.
type MacroOpBenchBaseline struct{}

// Run estimates output tokens for verbose Edit IR ops.
func (b *MacroOpBenchBaseline) Run(scenario MacroOpBenchScenario) MacroOpBenchResult {
	ir := &god.EditIR{Ops: scenario.VerboseEditIR}
	irJSON, _ := json.Marshal(ir)
	tokensOut := EstimateTokens(irJSON)

	return MacroOpBenchResult{
		Scenario:          scenario.Name,
		BaselineTokensOut: tokensOut,
		BaselineOps:       len(scenario.VerboseEditIR),
	}
}

// MacroOpBenchGenesis estimates tokens for Macro Ops output.
type MacroOpBenchGenesis struct{}

// Run estimates output tokens for Macro Ops.
func (g *MacroOpBenchGenesis) Run(scenario MacroOpBenchScenario) MacroOpBenchResult {
	macros := &god.MacroOps{Ops: scenario.MacroOps}
	macroJSON, _ := json.Marshal(macros)
	tokensOut := EstimateTokens(macroJSON)

	return MacroOpBenchResult{
		Scenario:         scenario.Name,
		GenesisTokensOut: tokensOut,
		GenesisOps:       len(scenario.MacroOps),
	}
}

// DefaultMacroOpBenchScenarios returns macro ops benchmark scenarios.
func DefaultMacroOpBenchScenarios() []MacroOpBenchScenario {
	return []MacroOpBenchScenario{
		{
			Name: "Rename symbol (10 occurrences)",
			VerboseEditIR: func() []god.EditOp {
				var ops []god.EditOp
				files := []string{"main.go", "handler.go", "service.go", "model.go", "repo.go",
					"main_test.go", "handler_test.go", "service_test.go", "model_test.go", "repo_test.go"}
				for i, f := range files {
					ops = append(ops, god.EditOp{
						Op:         "replace_span",
						Path:       f,
						AnchorHash: fmt.Sprintf("anchor-%d", i),
						Lines:      []int{i*10 + 5, i*10 + 5},
						Content:    fmt.Sprintf("func Compute(a, b float64) float64 { // was Calculate, occurrence %d", i),
					})
				}
				return ops
			}(),
			MacroOps: []god.MacroOp{{
				Kind:      god.MacroRenameSymbol,
				OldName:   "Calculate",
				NewName:   "Compute",
				ScopePath: "*",
			}},
		},
		{
			Name: "Add parameter (5 call sites)",
			VerboseEditIR: func() []god.EditOp {
				var ops []god.EditOp
				// Modify function signature
				ops = append(ops, god.EditOp{
					Op:         "replace_span",
					Path:       "service.go",
					AnchorHash: "sig-anchor",
					Lines:      []int{15, 15},
					Content:    "func Process(ctx context.Context, input string, opts Options) (Result, error) {",
				})
				// Modify 5 call sites
				for i := 0; i < 5; i++ {
					ops = append(ops, god.EditOp{
						Op:         "replace_span",
						Path:       fmt.Sprintf("caller_%d.go", i),
						AnchorHash: fmt.Sprintf("call-anchor-%d", i),
						Lines:      []int{20 + i*10, 20 + i*10},
						Content:    fmt.Sprintf("result, err := Process(ctx, input, DefaultOptions()) // call site %d", i),
					})
				}
				return ops
			}(),
			MacroOps: []god.MacroOp{{
				Kind:      god.MacroAddParam,
				FuncName:  "Process",
				ParamName: "opts",
				ParamType: "Options",
				Position:  2,
			}},
		},
		{
			Name: "Add import + function stub (3 files)",
			VerboseEditIR: func() []god.EditOp {
				var ops []god.EditOp
				files := []string{"handler.go", "service.go", "middleware.go"}
				for i, f := range files {
					// Each file needs import block rewritten
					ops = append(ops, god.EditOp{
						Op:         "replace_span",
						Path:       f,
						AnchorHash: fmt.Sprintf("import-anchor-%d", i),
						Lines:      []int{3, 5},
						Content:    "import (\n\t\"context\"\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"github.com/example/metrics\"\n)",
					})
					// Each file gets the function stub
					ops = append(ops, god.EditOp{
						Op:         "insert_after_symbol",
						Path:       f,
						AnchorHash: fmt.Sprintf("func-anchor-%d", i),
						Symbol:     "HandleRequest",
						Content:    fmt.Sprintf("\n\nfunc TrackMetrics_%s(ctx context.Context, name string, duration time.Duration) error {\n\treturn metrics.Record(ctx, name, duration)\n}\n", f[:len(f)-3]),
					})
					// Each file gets a call site
					ops = append(ops, god.EditOp{
						Op:         "replace_span",
						Path:       f,
						AnchorHash: fmt.Sprintf("call-anchor-%d", i),
						Lines:      []int{45 + i*10, 45 + i*10},
						Content:    fmt.Sprintf("\tdefer TrackMetrics_%s(ctx, \"handle_request\", time.Since(start))", f[:len(f)-3]),
					})
				}
				return ops
			}(),
			MacroOps: []god.MacroOp{
				{
					Kind:       god.MacroInsertImport,
					Path:       "handler.go",
					ImportSpec: "github.com/example/metrics",
				},
				{
					Kind:    god.MacroAddFunctionStub,
					Path:    "handler.go",
					FuncSig: "func TrackMetrics(ctx context.Context, name string, duration time.Duration) error",
				},
			},
		},
		{
			Name: "Add test cases (3 functions)",
			VerboseEditIR: func() []god.EditOp {
				var ops []god.EditOp
				funcs := []string{"Pow", "Sqrt", "Abs"}
				for i, fn := range funcs {
					ops = append(ops, god.EditOp{
						Op:         "insert_after_symbol",
						Path:       "math_test.go",
						AnchorHash: fmt.Sprintf("test-anchor-%d", i),
						Symbol:     "TestAdd",
						Content: fmt.Sprintf(`

func Test%s(t *testing.T) {
	tests := []struct {
		name string
		x, y float64
		want float64
	}{
		{"case1", 2, 3, 8},
		{"case2", 10, 0, 1},
		{"case3", 0, 5, 0},
		{"case4", 2, 10, 1024},
		{"case5", -1, 3, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := %s(tt.x, tt.y)
			if got != tt.want {
				t.Errorf("%s(%%v, %%v) = %%v, want %%v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}`, fn, fn, fn),
					})
				}
				return ops
			}(),
			MacroOps: []god.MacroOp{
				{
					Kind:     god.MacroAddTestCase,
					TestFunc: "TestPow",
					CaseName: "pow_cases",
					CaseBody: `{"2^3",2,3,8}, {"10^0",10,0,1}, {"0^5",0,5,0}`,
				},
				{
					Kind:     god.MacroAddTestCase,
					TestFunc: "TestSqrt",
					CaseName: "sqrt_cases",
					CaseBody: `{"4",4,0,2}, {"9",9,0,3}`,
				},
				{
					Kind:     god.MacroAddTestCase,
					TestFunc: "TestAbs",
					CaseName: "abs_cases",
					CaseBody: `{"-5",-5,0,5}, {"3",3,0,3}`,
				},
			},
		},
	}
}

// PrintMacroOpBenchReport prints a comparison table for Macro Op benchmarks.
func PrintMacroOpBenchReport(results []MacroOpBenchResult) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    MACRO OPS BENCH REPORT                                ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Scenario                          Baseline     Genesis      Ratio  PASS ║")
	fmt.Println("║                                    (ops/tokens) (ops/tokens)              ║")
	fmt.Println("║  ──────────────────────────────────────────────────────────────────────   ║")

	for _, r := range results {
		pass := "FAIL"
		if r.Pass {
			pass = "PASS"
		}
		fmt.Printf("║  %-36s %d/%-9d %d/%-9d %-5.1fx %s ║\n",
			r.Scenario,
			r.BaselineOps, r.BaselineTokensOut,
			r.GenesisOps, r.GenesisTokensOut,
			r.Ratio, pass)
	}

	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}
