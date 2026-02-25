package bench

import (
	"encoding/json"
	"fmt"
	"os"
)

// CombinedBenchResult holds the end-to-end pipeline comparison.
type CombinedBenchResult struct {
	BaselineTotalTokens int     `json:"baseline_total_tokens"`
	GenesisTotalTokens  int     `json:"genesis_total_tokens"`
	TotalRatio          float64 `json:"total_ratio"`
	ISAInputRatio       float64 `json:"isa_input_ratio"`
	MacroOpsOutputRatio float64 `json:"macro_ops_output_ratio"`
	Pass                bool    `json:"pass"` // total >= 5.0
}

// RunCombinedBench computes the combined ISA + Macro Ops pipeline savings.
func RunCombinedBench(isaResults []ISABenchResult, macroResults []MacroOpBenchResult) CombinedBenchResult {
	// Sum up all baseline and genesis tokens across both dimensions
	totalBaselineIn := 0
	totalGenesisIn := 0
	for _, r := range isaResults {
		totalBaselineIn += r.BaselineTokensIn
		totalGenesisIn += r.GenesisTokensIn
	}

	totalBaselineOut := 0
	totalGenesisOut := 0
	for _, r := range macroResults {
		totalBaselineOut += r.BaselineTokensOut
		totalGenesisOut += r.GenesisTokensOut
	}

	baselineTotal := totalBaselineIn + totalBaselineOut
	genesisTotal := totalGenesisIn + totalGenesisOut

	totalRatio := 0.0
	if genesisTotal > 0 {
		totalRatio = float64(baselineTotal) / float64(genesisTotal)
	}

	isaRatio := 0.0
	if totalGenesisIn > 0 {
		isaRatio = float64(totalBaselineIn) / float64(totalGenesisIn)
	}

	macroRatio := 0.0
	if totalGenesisOut > 0 {
		macroRatio = float64(totalBaselineOut) / float64(totalGenesisOut)
	}

	return CombinedBenchResult{
		BaselineTotalTokens: baselineTotal,
		GenesisTotalTokens:  genesisTotal,
		TotalRatio:          totalRatio,
		ISAInputRatio:       isaRatio,
		MacroOpsOutputRatio: macroRatio,
		Pass:                totalRatio >= 5.0,
	}
}

// PrintCombinedBenchReport prints the combined pipeline report.
func PrintCombinedBenchReport(combined CombinedBenchResult) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  COMBINED PIPELINE BENCH REPORT                         ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Baseline total tokens:  %-10d                                      ║\n", combined.BaselineTotalTokens)
	fmt.Printf("║  Genesis total tokens:   %-10d                                      ║\n", combined.GenesisTotalTokens)
	fmt.Println("║                                                                          ║")
	fmt.Printf("║  ISA input ratio:        %-8.1fx                                       ║\n", combined.ISAInputRatio)
	fmt.Printf("║  Macro Ops output ratio: %-8.1fx                                       ║\n", combined.MacroOpsOutputRatio)
	fmt.Printf("║  Combined total ratio:   %-8.1fx   %s                              ║\n",
		combined.TotalRatio, passSymbol(combined.Pass))
	fmt.Println("║                                                                          ║")

	if combined.Pass {
		fmt.Println("║  VERDICT: PASS — Combined pipeline >= 5x total token savings            ║")
	} else if combined.TotalRatio >= 3.0 {
		fmt.Println("║  VERDICT: MARGINAL — Combined pipeline >= 3x but < 5x target            ║")
	} else {
		fmt.Println("║  VERDICT: FAIL — Combined pipeline < 3x total token savings             ║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

// CIReport aggregates all benchmark results for CI consumption.
type CIReport struct {
	Timestamp string               `json:"timestamp"`
	ISA       []ISABenchResult     `json:"isa"`
	MacroOps  []MacroOpBenchResult `json:"macro_ops"`
	Combined  *CombinedBenchResult `json:"combined"`
	Pass      bool                 `json:"pass"`
}

// WriteJSONReport writes the CI report to a JSON file.
func WriteJSONReport(report CIReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("ci report: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
