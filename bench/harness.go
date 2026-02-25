package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BenchResult holds metrics for one approach on one task.
type BenchResult struct {
	Approach    string `json:"approach"`     // "baseline" or "genesis"
	Task        string `json:"task"`
	TokensIn    int    `json:"tokens_in"`    // estimated input tokens
	TokensOut   int    `json:"tokens_out"`   // estimated output tokens
	Calls       int    `json:"calls"`        // provider calls
	Missions    int    `json:"missions"`     // mission count (genesis) or agent count (baseline)
	PassRate    float64 `json:"pass_rate"`   // 0.0 - 1.0
	ConflictRate float64 `json:"conflict_rate"` // fraction of missions with conflicts
	DurationMS  int64  `json:"duration_ms"`
	PackBytes   int    `json:"pack_bytes"`   // total bytes in packs/prompts
	OutputBytes int    `json:"output_bytes"` // total bytes in outputs
}

// BenchReport compares baseline vs genesis results on the same tasks.
type BenchReport struct {
	Timestamp string        `json:"timestamp"`
	Fixture   string        `json:"fixture"`
	Tasks     []TaskReport  `json:"tasks"`
}

// TaskReport is a pair of results for one task.
type TaskReport struct {
	Task     string      `json:"task"`
	Baseline BenchResult `json:"baseline"`
	Genesis  BenchResult `json:"genesis"`
}

// PrintReport prints a clean comparison table to stdout.
func PrintReport(report BenchReport) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    GENESIS HARDMODE BENCH REPORT                        ║")
	fmt.Printf("║  Fixture: %-30s  %s  ║\n", report.Fixture, report.Timestamp)
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")

	for _, tr := range report.Tasks {
		b := tr.Baseline
		g := tr.Genesis

		tokInRatio := safeDiv(b.TokensIn, g.TokensIn)
		tokOutRatio := safeDiv(b.TokensOut, g.TokensOut)
		callRatio := safeDiv(b.Calls, g.Calls)
		packRatio := safeDiv(b.PackBytes, g.PackBytes)

		fmt.Printf("║  Task: %-60s  ║\n", tr.Task)
		fmt.Println("║                                                                          ║")
		fmt.Println("║  Metric            Baseline       Genesis        Ratio     PASS?          ║")
		fmt.Println("║  ──────────────────────────────────────────────────────────────────────    ║")
		fmt.Printf("║  tokens_in         %-14d %-14d %-9.1fx %s     ║\n",
			b.TokensIn, g.TokensIn, tokInRatio, passSymbol(tokInRatio >= 5.0))
		fmt.Printf("║  tokens_out        %-14d %-14d %-9.1fx %s     ║\n",
			b.TokensOut, g.TokensOut, tokOutRatio, passSymbol(tokOutRatio >= 2.0))
		fmt.Printf("║  calls             %-14d %-14d %-9.1fx %s     ║\n",
			b.Calls, g.Calls, callRatio, passSymbol(callRatio >= 0.75))
		fmt.Printf("║  pack_bytes        %-14d %-14d %-9.1fx          ║\n",
			b.PackBytes, g.PackBytes, packRatio)
		fmt.Printf("║  missions/agents   %-14d %-14d                          ║\n",
			b.Missions, g.Missions)
		fmt.Printf("║  pass_rate         %-14.0f%% %-14.0f%%                         ║\n",
			b.PassRate*100, g.PassRate*100)
		fmt.Printf("║  conflict_rate     %-14.0f%% %-14.0f%%                         ║\n",
			b.ConflictRate*100, g.ConflictRate*100)
		fmt.Println("║                                                                          ║")

		// Overall verdict
		pass := tokInRatio >= 5.0 || tokInRatio >= 3.0
		if pass {
			fmt.Println("║  VERDICT: PASS — Genesis beats baseline on token economics              ║")
		} else {
			fmt.Println("║  VERDICT: FAIL — Genesis does not meet 3x minimum threshold             ║")
		}
		fmt.Println("║──────────────────────────────────────────────────────────────────────────║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

// PrintSingleAgentReport prints a comparison table for single-agent benchmarks.
func PrintSingleAgentReport(report BenchReport) {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║               GENESIS SINGLE-AGENT BENCH REPORT                         ║")
	fmt.Printf("║  Fixture: %-30s  %s  ║\n", report.Fixture, report.Timestamp)
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")

	for _, tr := range report.Tasks {
		b := tr.Baseline
		g := tr.Genesis

		tokInRatio := safeDiv(b.TokensIn, g.TokensIn)
		tokOutRatio := safeDiv(b.TokensOut, g.TokensOut)
		bTotal := b.TokensIn + b.TokensOut
		gTotal := g.TokensIn + g.TokensOut
		totalRatio := 0.0
		if gTotal > 0 {
			totalRatio = float64(bTotal) / float64(gTotal)
		}

		fmt.Printf("║  Task: %-60s  ║\n", tr.Task)
		fmt.Println("║                                                                          ║")
		fmt.Println("║  Metric            Baseline       Genesis        Ratio     PASS?          ║")
		fmt.Println("║  ──────────────────────────────────────────────────────────────────────    ║")
		fmt.Printf("║  tokens_in         %-14d %-14d %-9.1fx %s     ║\n",
			b.TokensIn, g.TokensIn, tokInRatio, passSymbol(tokInRatio >= 5.0))
		fmt.Printf("║  tokens_out        %-14d %-14d %-9.1fx %s     ║\n",
			b.TokensOut, g.TokensOut, tokOutRatio, passSymbol(tokOutRatio >= 2.0))
		fmt.Printf("║  total_ratio       %-14d %-14d %-9.1fx %s     ║\n",
			bTotal, gTotal, totalRatio, passSymbol(totalRatio >= 5.0))
		fmt.Printf("║  calls             %-14d %-14d                          ║\n",
			b.Calls, g.Calls)
		fmt.Printf("║  pack_bytes        %-14d %-14d %-9.1fx          ║\n",
			b.PackBytes, g.PackBytes, safeDiv(b.PackBytes, g.PackBytes))
		fmt.Printf("║  pass_rate         %-14.0f%% %-14.0f%%                         ║\n",
			b.PassRate*100, g.PassRate*100)
		fmt.Println("║                                                                          ║")

		if totalRatio >= 5.0 {
			fmt.Println("║  VERDICT: PASS — Genesis single-agent >= 5x total token savings          ║")
		} else if totalRatio >= 3.0 {
			fmt.Println("║  VERDICT: PASS — Genesis single-agent >= 3x total token savings          ║")
		} else {
			fmt.Println("║  VERDICT: FAIL — Genesis single-agent < 3x total token savings           ║")
		}
		fmt.Println("║──────────────────────────────────────────────────────────────────────────║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝")
}

func safeDiv(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func passSymbol(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// EstimateTokens mirrors god.EstimateTokens: bytes/4 + 10 overhead.
func EstimateTokens(data []byte) int {
	return len(data)/4 + 10
}

// CollectSourceFiles walks a repo and returns all source file paths and their combined size.
func CollectSourceFiles(repoRoot string) (paths []string, totalBytes int, err error) {
	codeExts := map[string]bool{
		".go": true, ".py": true, ".ts": true, ".js": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true,
		".java": true, ".rb": true, ".toml": true, ".json": true,
	}
	err = filepath.Walk(repoRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		// Skip test fixture subdirectories
		rel, _ := filepath.Rel(repoRoot, path)
		if strings.HasPrefix(rel, "e2e_calc_repo") || strings.HasPrefix(rel, "ir_small_repo") {
			return nil
		}
		ext := filepath.Ext(path)
		if codeExts[ext] {
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				paths = append(paths, rel)
				totalBytes += len(data)
			}
		}
		return nil
	})
	return
}

// ReadFile reads a file from a repo root by relative path.
func ReadFile(repoRoot, relPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(repoRoot, relPath))
}

// ToJSON marshals to pretty JSON.
func ToJSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data)
}

// Now returns formatted timestamp.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
