package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/genesis-ssmp/genesis/god"
	"github.com/genesis-ssmp/genesis/heaven"
)

const defaultAddr = "127.0.0.1:4444"
const defaultDataDir = ".genesis"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "init":
		cmdInit()
	case "index":
		cmdIndex()
	case "run":
		cmdRun()
	case "status":
		cmdStatus()
	case "logs":
		cmdLogs()
	case "serve":
		cmdServe()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Genesis SSMP CLI

Usage: genesis <command> [options]

Commands:
  init              Initialize a Genesis workspace in the current directory
  index             Build the IR index for the current repository
  run "task text"   Plan and execute a task end-to-end
  status            Show Heaven daemon status
  logs              Show recent event log entries
  serve             Start the Heaven daemon

Options:
  --addr ADDR       Heaven daemon address (default: 127.0.0.1:4444)
  --data-dir DIR    Data directory (default: .genesis)`)
}

// cmdInit initializes a Genesis workspace.
func cmdInit() {
	dataDir := flagOrDefault("--data-dir", defaultDataDir)

	fmt.Println("[genesis] Initializing workspace...")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fatal("create data dir: %v", err)
	}

	// Write a minimal config marker
	configPath := filepath.Join(dataDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := map[string]string{
			"heaven_addr": defaultAddr,
			"data_dir":    dataDir,
		}
		data, _ := json.MarshalIndent(config, "", "  ")
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			fatal("write config: %v", err)
		}
	}

	fmt.Printf("[genesis] Workspace initialized at %s\n", dataDir)
	fmt.Println("[genesis] Run 'genesis serve' to start the Heaven daemon")
}

// cmdIndex builds the IR index.
func cmdIndex() {
	addr := flagOrDefault("--addr", defaultAddr)
	repoPath, _ := os.Getwd()
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "--") {
		repoPath = os.Args[2]
	}

	client := god.NewHeavenClient("http://" + addr)

	fmt.Printf("[genesis] Indexing %s...\n", repoPath)
	filesIndexed, err := client.IRBuild(repoPath)
	if err != nil {
		fatal("index: %v", err)
	}

	fmt.Printf("[genesis] Indexed %d files\n", filesIndexed)
}

// cmdRun plans and executes a task end-to-end.
func cmdRun() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: genesis run \"task description\"")
		os.Exit(1)
	}

	taskDesc := os.Args[2]
	addr := flagOrDefault("--addr", defaultAddr)
	repoPath, _ := os.Getwd()

	client := god.NewHeavenClient("http://" + addr)
	planner := god.NewPlanner(client)
	integrator := god.NewIntegrator(client)
	verifier := god.NewVerifier(client)
	metrics := god.NewMetricsAggregator(client)

	// Step 1: Plan
	fmt.Printf("[genesis] Planning: %s\n", taskDesc)
	dag, err := planner.Plan(taskDesc, repoPath)
	if err != nil {
		fatal("plan: %v", err)
	}

	fmt.Printf("[genesis] Created %d missions (plan %s)\n", len(dag.Nodes), dag.PlanID)
	for _, node := range dag.Nodes {
		fmt.Printf("  - %s: %s\n", node.Mission.MissionID[:8], node.Mission.Goal)
	}

	// Step 2: Execute each mission
	compiler := god.NewPromptCompiler("http://" + addr + "/pf")
	for _, node := range dag.Nodes {
		m := node.Mission

		// Build candidate shards from IR for this mission
		var candidates []god.CandidateShard
		for _, scope := range m.Scopes {
			if scope.ScopeType == "symbol" {
				syms, err := client.IRSymdef(scope.ScopeValue)
				if err == nil && len(syms) > 0 {
					sym := syms[0]
					content, _ := json.Marshal(map[string]any{
						"symbol": sym.Name, "kind": sym.Kind, "path": sym.Path,
						"lines": []int{sym.StartLine, sym.EndLine},
					})
					candidates = append(candidates, god.CandidateShard{
						Kind: "symdef", BlobID: fmt.Sprintf("symdef-%s-%d", scope.ScopeValue, sym.ID),
						Content: content, Symbol: scope.ScopeValue, Path: sym.Path,
					})
					refs, _ := client.IRCallers(scope.ScopeValue, 5)
					if len(refs) > 0 {
						refContent, _ := json.Marshal(map[string]any{"symbol": scope.ScopeValue, "callers": refs})
						candidates = append(candidates, god.CandidateShard{
							Kind: "callers", BlobID: fmt.Sprintf("callers-%s", scope.ScopeValue),
							Content: refContent, Symbol: scope.ScopeValue,
						})
					}
				}
			}
		}

		// Compile the MissionPack
		pack, packErr := compiler.Compile(m, candidates)

		metrics.StartMission(m.MissionID)
		fmt.Printf("\n[genesis] Executing mission %s...\n", m.MissionID[:8])
		fmt.Printf("  Goal: %s\n", m.Goal)

		if packErr == nil && pack != nil {
			packJSON, _ := json.Marshal(pack)
			fmt.Printf("  Pack: %d bytes, %d tokens (budget: %d)\n",
				len(packJSON), pack.BudgetMeta.TotalTokens, pack.BudgetMeta.TokenBudget)
			fmt.Printf("  Shards: %d included, %d dropped\n",
				pack.BudgetMeta.ShardsIncluded, pack.BudgetMeta.ShardsDropped)
			fmt.Printf("  Header: %d tok, Mission: %d tok, Shards: %d tok\n",
				pack.BudgetMeta.HeaderTokens, pack.BudgetMeta.MissionTokens, pack.BudgetMeta.ShardTokens)

			// Dump pack JSON if requested
			if os.Getenv("GENESIS_DUMP_PACK") != "" {
				prettyJSON, _ := json.MarshalIndent(pack, "", "  ")
				dumpPath := fmt.Sprintf("/tmp/genesis-pack-%s.json", m.MissionID)
				_ = os.WriteFile(dumpPath, prettyJSON, 0644)
				fmt.Printf("  Pack dumped to: %s (%d bytes pretty)\n", dumpPath, len(prettyJSON))
			}
		}

		client.AppendEvent(map[string]any{
			"type":       "mission_dispatched",
			"mission_id": m.MissionID,
			"goal":       m.Goal,
		})

		metrics.EndTurn(m.MissionID)
		metrics.CompleteMission(m.MissionID)
		fmt.Printf("  [done]\n")
	}

	// Step 3: Run verification
	fmt.Printf("\n[genesis] Running verification...\n")
	result, err := verifier.Verify(god.VerifyRequest{
		MissionID: dag.PlanID,
		RepoRoot:  repoPath,
	})
	if err != nil {
		fmt.Printf("  Verification error: %v\n", err)
	} else {
		if result.Passed {
			fmt.Printf("  Tests passed (receipt %s)\n", result.BlobID)
		} else {
			fmt.Printf("  Tests failed (exit code %d)\n", result.Receipt.ExitCode)
		}
	}

	// Step 4: Show metrics
	fmt.Printf("\n[genesis] Metrics summary:\n")
	for _, node := range dag.Nodes {
		summary := metrics.Summary(node.Mission.MissionID)
		for _, line := range strings.Split(summary, "\n") {
			if line != "" {
				fmt.Printf("  %s\n", line)
			}
		}
	}

	// Step 5: Gate merge
	if result != nil {
		if err := god.GateMerge(result.Receipt); err != nil {
			fmt.Printf("\n[genesis] Merge gate: BLOCKED (%v)\n", err)
		} else {
			fmt.Printf("\n[genesis] Merge gate: PASSED\n")
		}
	}

	_ = integrator // available for when provider is configured
}

// cmdStatus shows Heaven daemon status.
func cmdStatus() {
	addr := flagOrDefault("--addr", defaultAddr)
	client := god.NewHeavenClient("http://" + addr)

	status, err := client.GetStatus()
	if err != nil {
		fatal("status: %v", err)
	}

	fmt.Printf("[genesis] Heaven status:\n")
	fmt.Printf("  state_rev:      %d\n", status.StateRev)
	fmt.Printf("  active_leases:  %d\n", status.ActiveLeasesCount)
	if len(status.FileClockSummary) > 0 {
		fmt.Printf("  file_clocks:\n")
		for path, clock := range status.FileClockSummary {
			fmt.Printf("    %s: %s\n", path, clock)
		}
	}
}

// cmdLogs shows recent events.
func cmdLogs() {
	addr := flagOrDefault("--addr", defaultAddr)
	n := 20
	client := god.NewHeavenClient("http://" + addr)

	events, err := client.TailEvents(n)
	if err != nil {
		fatal("logs: %v", err)
	}

	if len(events) == 0 {
		fmt.Println("[genesis] No events recorded")
		return
	}

	fmt.Printf("[genesis] Last %d events:\n", len(events))
	for i, raw := range events {
		var evt map[string]any
		json.Unmarshal(raw, &evt)
		evtType, _ := evt["type"].(string)
		missionID, _ := evt["mission_id"].(string)

		prefix := ""
		if missionID != "" && len(missionID) >= 8 {
			prefix = missionID[:8] + " "
		}
		fmt.Printf("  %3d. %s%s\n", i+1, prefix, evtType)
	}
}

// cmdServe starts the Heaven daemon.
func cmdServe() {
	addr := flagOrDefault("--addr", defaultAddr)
	dataDir := flagOrDefault("--data-dir", defaultDataDir)

	fmt.Printf("[genesis] Starting Heaven daemon on %s (data: %s)\n", addr, dataDir)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		fatal("create data dir: %v", err)
	}

	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		fatal("server init: %v", err)
	}

	fmt.Printf("[genesis] Heaven is listening on %s\n", addr)
	if err := srv.ListenAndServe(addr); err != nil {
		fatal("serve: %v", err)
	}
}

// flagOrDefault scans os.Args for --key value and returns the value or default.
func flagOrDefault(flag, defaultVal string) string {
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return defaultVal
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[genesis] ERROR: "+format+"\n", args...)
	os.Exit(1)
}
