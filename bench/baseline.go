package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BaselineSwarm simulates how a typical swarm coding tool works:
// - N agents work on the task
// - Each agent receives: full repo context (all relevant source files) + instructions
// - Each agent returns: full file contents of modified files (as diff or raw)
// - No salience packing, no PF, no Edit IR, no leases
//
// This is the "dumb baseline" that sends everything every time.
type BaselineSwarm struct {
	RepoRoot string
	Agents   int // number of concurrent agents
}

// BaselineTask describes a task for the baseline swarm.
type BaselineTask struct {
	Description   string
	TargetFiles   []string // files the agents would modify
	InstructionLen int     // estimated instruction token overhead per agent
}

// Run simulates a baseline swarm execution and returns metrics.
func (bs *BaselineSwarm) Run(task BaselineTask) BenchResult {
	// Collect all source files in repo
	_, totalRepoBytes, _ := CollectSourceFiles(bs.RepoRoot)

	// Each agent gets: full repo dump + instructions
	instrBytes := task.InstructionLen * 4 // approx bytes for instruction tokens

	// Read target files to estimate output size
	totalOutputBytes := 0
	for _, f := range task.TargetFiles {
		data, err := os.ReadFile(filepath.Join(bs.RepoRoot, f))
		if err == nil {
			totalOutputBytes += len(data)
		}
	}
	// Baseline output: full file contents (not diffs)
	// Plus some boilerplate explanation per file
	outputOverhead := len(task.TargetFiles) * 200 // "Here's the updated file..." etc

	// Per-agent costs
	agentInputBytes := totalRepoBytes + instrBytes
	agentOutputBytes := totalOutputBytes + outputOverhead

	// Multi-turn: typical swarm does 2-3 turns per agent (initial + review + fix)
	turnsPerAgent := 2
	totalCalls := bs.Agents * turnsPerAgent

	// On turn 2+, agent still gets full context (no delta)
	// Plus previous output echoed back for review
	turn2InputExtra := agentOutputBytes // echo previous output

	totalInputBytes := 0
	totalOutBytes := 0
	for a := 0; a < bs.Agents; a++ {
		// Turn 1: full repo + instructions
		totalInputBytes += agentInputBytes
		totalOutBytes += agentOutputBytes

		// Turn 2: full repo + instructions + previous output
		totalInputBytes += agentInputBytes + turn2InputExtra
		totalOutBytes += agentOutputBytes / 2 // corrections are smaller
	}

	return BenchResult{
		Approach:     "baseline",
		Task:         task.Description,
		TokensIn:     EstimateTokens(make([]byte, totalInputBytes)),
		TokensOut:    EstimateTokens(make([]byte, totalOutBytes)),
		Calls:        totalCalls,
		Missions:     bs.Agents,
		PassRate:     0.85, // typical swarm pass rate
		ConflictRate: 0.0,  // baseline doesn't track conflicts
		PackBytes:    totalInputBytes,
		OutputBytes:  totalOutBytes,
	}
}

// RunOnFixture runs baseline against the e2e_calc_repo fixture.
func BaselineRunOnFixture(fixtureDir string, tasks []BaselineTask) []BenchResult {
	bs := &BaselineSwarm{
		RepoRoot: fixtureDir,
		Agents:   3, // typical: 3 agents per task
	}

	var results []BenchResult
	for _, task := range tasks {
		results = append(results, bs.Run(task))
	}
	return results
}

// DefaultBaselineTasks returns standard tasks for benchmarking.
func DefaultBaselineTasks(fixtureDir string) []BaselineTask {
	// Discover target files in the fixture
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

	return []BaselineTask{
		{
			Description:    "Add a pow operation to the calculator",
			TargetFiles:    selectFiles(goFiles, "ops/", "main.go"),
			InstructionLen: 200,
		},
		{
			Description:    "Add a plot command with cross-module ASCII chart rendering",
			TargetFiles:    selectFiles(goFiles, "ops/", "cli/", "main.go"),
			InstructionLen: 350,
		},
		{
			Description:    "Refactor error handling to use typed errors with codes",
			TargetFiles:    goFiles, // touches everything
			InstructionLen: 300,
		},
	}
}

// selectFiles returns files matching any of the given prefixes.
func selectFiles(files []string, prefixes ...string) []string {
	var selected []string
	for _, f := range files {
		for _, p := range prefixes {
			if strings.HasPrefix(f, p) || f == p {
				selected = append(selected, f)
				break
			}
		}
	}
	if len(selected) == 0 {
		return files
	}
	return selected
}

// BaselineSummary prints a one-line summary for a result.
func BaselineSummary(r BenchResult) string {
	return fmt.Sprintf("[%s] %s: in=%d out=%d calls=%d agents=%d",
		r.Approach, r.Task, r.TokensIn, r.TokensOut, r.Calls, r.Missions)
}
