package bench

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/genesis-ssmp/genesis/internal/lean"
)

// LeanBenchScenario describes a TSLN vs JSON comparison scenario.
type LeanBenchScenario struct {
	Name         string
	Description  string
	SnapshotCount int
	Generator    func(n int) []lean.MetricsRow
}

// LeanBenchResult holds the outcome of one scenario.
type LeanBenchResult struct {
	Scenario   string  `json:"scenario"`
	JSONBytes  int     `json:"json_bytes"`
	TSLNBytes  int     `json:"tsln_bytes"`
	JSONTokens int     `json:"json_tokens"`
	TSLNTokens int     `json:"tsln_tokens"`
	Ratio      float64 `json:"ratio"`     // TSLN / JSON
	Reduction  float64 `json:"reduction"` // percentage saved
	Pass       bool    `json:"pass"`
	Threshold  float64 `json:"threshold"` // max ratio to pass
}

// DefaultLeanScenarios returns the standard benchmark scenarios.
func DefaultLeanScenarios() []LeanBenchScenario {
	return []LeanBenchScenario{
		{
			Name:          "single_snapshot",
			Description:   "Oracle escalation: 1 metrics snapshot",
			SnapshotCount: 1,
			Generator:     generateActiveSnapshots,
			// Single row has header overhead, expect roughly break-even
		},
		{
			Name:          "5_turn_mission",
			Description:   "Normal mission: 5 incremental snapshots",
			SnapshotCount: 5,
			Generator:     generateActiveSnapshots,
		},
		{
			Name:          "20_turn_thrashing",
			Description:   "Thrashing mission: 20 snapshots with status change",
			SnapshotCount: 20,
			Generator:     generateThrashingSnapshots,
		},
		{
			Name:          "50_turn_long_run",
			Description:   "Long-running mission: 50 incremental snapshots",
			SnapshotCount: 50,
			Generator:     generateActiveSnapshots,
		},
	}
}

// RunLeanBench executes one benchmark scenario and returns results.
func RunLeanBench(scenario LeanBenchScenario) LeanBenchResult {
	rows := scenario.Generator(scenario.SnapshotCount)

	// JSON encoding: array of metric objects
	jsonRows := metricsRowsToJSON(rows)
	jsonData, _ := json.Marshal(jsonRows)
	jsonBytes := len(jsonData)

	// TSLN encoding
	tslnData, err := lean.EncodeMetricsRows(rows)
	if err != nil {
		return LeanBenchResult{Scenario: scenario.Name, Pass: false}
	}
	tslnBytes := len(tslnData)

	ratio := float64(tslnBytes) / float64(jsonBytes)

	// Thresholds: single row is break-even, multi-row should show gains
	threshold := 1.1 // default: allow 10% overhead
	if scenario.SnapshotCount >= 3 {
		threshold = 0.50 // 3+ rows: must be under 50%
	}
	if scenario.SnapshotCount >= 10 {
		threshold = 0.35 // 10+ rows: must be under 35%
	}

	return LeanBenchResult{
		Scenario:   scenario.Name,
		JSONBytes:  jsonBytes,
		TSLNBytes:  tslnBytes,
		JSONTokens: jsonBytes / 4,
		TSLNTokens: tslnBytes / 4,
		Ratio:      ratio,
		Reduction:  (1.0 - ratio) * 100,
		Pass:       ratio <= threshold,
		Threshold:  threshold,
	}
}

// PrintLeanBenchReport prints a formatted comparison table.
func PrintLeanBenchReport(results []LeanBenchResult) {
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      GENESIS LEAN STREAM BENCH REPORT                        ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ %-22s │ %8s │ %8s │ %8s │ %8s │ %4s ║\n",
		"Scenario", "JSON tok", "TSLN tok", "Ratio", "Saved", "Pass")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════════════════╣")

	allPass := true
	for _, r := range results {
		passStr := " OK "
		if !r.Pass {
			passStr = "FAIL"
			allPass = false
		}
		fmt.Printf("║ %-22s │ %8d │ %8d │ %7.1f%% │ %7.1f%% │ %4s ║\n",
			r.Scenario, r.JSONTokens, r.TSLNTokens, r.Ratio*100, r.Reduction, passStr)
	}

	fmt.Println("╠═══════════════════════════════════════════════════════════════════════════════╣")
	if allPass {
		fmt.Println("║ RESULT: ALL SCENARIOS PASSED                                                ║")
	} else {
		fmt.Println("║ RESULT: SOME SCENARIOS FAILED                                               ║")
	}
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════════════╝")
}

// --- Generators ---

func generateActiveSnapshots(n int) []lean.MetricsRow {
	base := time.Date(2026, 2, 8, 10, 0, 0, 0, time.UTC)
	rows := make([]lean.MetricsRow, n)
	for i := range rows {
		rows[i] = lean.MetricsRow{
			Timestamp:        base.Add(time.Duration(i) * time.Second),
			MissionID:        "bench-mission-001",
			Status:           "active",
			PFCount:          5 + i*3,
			PFResponseSize:   4200 + i*2800,
			Retries:          i / 4,
			Rejects:          0,
			Conflicts:        0,
			TestFailures:     0,
			TokensIn:         1200 + i*800,
			TokensOut:        300 + i*150,
			Turns:            i + 1,
			ElapsedMS:        int64(1500 + i*1200),
			PhaseTransitions: i / 5,
		}
	}
	return rows
}

func generateThrashingSnapshots(n int) []lean.MetricsRow {
	base := time.Date(2026, 2, 8, 10, 0, 0, 0, time.UTC)
	rows := make([]lean.MetricsRow, n)
	for i := range rows {
		status := "active"
		if i >= n*3/4 {
			status = "thrashing"
		}
		rows[i] = lean.MetricsRow{
			Timestamp:        base.Add(time.Duration(i) * time.Second),
			MissionID:        "bench-thrash-001",
			Status:           status,
			PFCount:          5 + i*4,
			PFResponseSize:   4200 + i*3200,
			Retries:          i / 3,
			Rejects:          i / 5,
			Conflicts:        i / 7,
			TestFailures:     i / 4,
			TokensIn:         1200 + i*900,
			TokensOut:        300 + i*200,
			Turns:            i + 1,
			ElapsedMS:        int64(1500 + i*1400),
			PhaseTransitions: i / 6,
		}
	}
	return rows
}

// jsonMetricsRow is the JSON-equivalent of a metrics snapshot for benchmarking.
type jsonMetricsRow struct {
	MissionID      string `json:"mission_id"`
	Status         string `json:"status"`
	PFCount        int    `json:"pf_count"`
	PFResponseSize int    `json:"pf_response_size_total"`
	Retries        int    `json:"retries"`
	Rejects        int    `json:"rejects"`
	Conflicts      int    `json:"conflicts"`
	TestFailures   int    `json:"test_failures"`
	TokensIn       int    `json:"tokens_in"`
	TokensOut      int    `json:"tokens_out"`
	Turns          int    `json:"turns"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	PhaseTx        int    `json:"phase_transitions"`
	StartedAt      string `json:"started_at"`
}

func metricsRowsToJSON(rows []lean.MetricsRow) []jsonMetricsRow {
	out := make([]jsonMetricsRow, len(rows))
	for i, r := range rows {
		out[i] = jsonMetricsRow{
			MissionID:      r.MissionID,
			Status:         r.Status,
			PFCount:        r.PFCount,
			PFResponseSize: r.PFResponseSize,
			Retries:        r.Retries,
			Rejects:        r.Rejects,
			Conflicts:      r.Conflicts,
			TestFailures:   r.TestFailures,
			TokensIn:       r.TokensIn,
			TokensOut:      r.TokensOut,
			Turns:          r.Turns,
			ElapsedMS:      r.ElapsedMS,
			PhaseTx:        r.PhaseTransitions,
			StartedAt:      r.Timestamp.Format(time.RFC3339),
		}
	}
	return out
}
