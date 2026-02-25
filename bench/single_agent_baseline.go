package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SingleAgentBaseline simulates a naive single-agent coding workflow:
// - 1 agent, multi-turn conversation
// - Each turn: full repo context + instructions + previous output echo
// - Output: full file content of modified files (not diffs, not Edit IR)
// - No salience scoring, no PF, no leases, no Edit IR
type SingleAgentBaseline struct {
	RepoRoot string
}

// SingleAgentBaselineTask describes a task for the single-agent baseline.
type SingleAgentBaselineTask struct {
	Description    string
	TargetFiles    []string // files the agent would modify
	InstructionLen int      // instruction overhead in tokens
	Turns          int      // number of conversation turns (default 2)
}

// Run simulates a single-agent baseline execution.
func (sab *SingleAgentBaseline) Run(task SingleAgentBaselineTask) BenchResult {
	_, totalRepoBytes, _ := CollectSourceFiles(sab.RepoRoot)

	instrBytes := task.InstructionLen * 4 // tokens -> bytes
	turns := task.Turns
	if turns == 0 {
		turns = 2
	}

	// Read target files to estimate output size
	totalTargetBytes := 0
	for _, f := range task.TargetFiles {
		data, err := os.ReadFile(filepath.Join(sab.RepoRoot, f))
		if err == nil {
			totalTargetBytes += len(data)
		}
	}

	// Output per turn: full content of modified files + explanation boilerplate
	outputPerTurn := totalTargetBytes + len(task.TargetFiles)*200

	totalInputBytes := 0
	totalOutputBytes := 0

	for turn := 0; turn < turns; turn++ {
		// Input: full repo context + instructions
		turnInput := totalRepoBytes + instrBytes
		if turn > 0 {
			// Subsequent turns also echo previous output for context
			turnInput += outputPerTurn
		}
		totalInputBytes += turnInput

		// Output: full file contents (smaller on correction turns)
		if turn == 0 {
			totalOutputBytes += outputPerTurn
		} else {
			totalOutputBytes += outputPerTurn * 3 / 4 // corrections slightly smaller
		}
	}

	return BenchResult{
		Approach:    "baseline-single",
		Task:        task.Description,
		TokensIn:    EstimateTokens(make([]byte, totalInputBytes)),
		TokensOut:   EstimateTokens(make([]byte, totalOutputBytes)),
		Calls:       turns,
		Missions:    1,
		PassRate:    0.90, // single agent typically better than swarm on small tasks
		PackBytes:   totalInputBytes,
		OutputBytes: totalOutputBytes,
	}
}

// DefaultSingleAgentBaselineTasks returns tasks A and B for single-agent benchmarking.
func DefaultSingleAgentBaselineTasks(fixtureDir string) []SingleAgentBaselineTask {
	var goFiles []string
	filepath.Walk(fixtureDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			rel, _ := filepath.Rel(fixtureDir, path)
			goFiles = append(goFiles, rel)
		}
		return nil
	})

	return []SingleAgentBaselineTask{
		{
			Description:    "Add pow(x,y) op + tests + CLI command",
			TargetFiles:    selectFiles(goFiles, "ops/", "cli/", "main.go"),
			InstructionLen: 250,
			Turns:          2,
		},
		{
			Description:    "Add plot(sin(x),0..10,200 samples) ascii render + tests",
			TargetFiles:    goFiles, // cross-module: touches many files
			InstructionLen: 400,
			Turns:          3, // medium task needs more turns
		},
	}
}

// SingleAgentBaselineSummary formats a one-line summary.
func SingleAgentBaselineSummary(r BenchResult) string {
	return fmt.Sprintf("[%s] %s: in=%d out=%d calls=%d",
		r.Approach, r.Task, r.TokensIn, r.TokensOut, r.Calls)
}
