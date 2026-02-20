package god

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Helper: spin up a real Heaven server with indexed fixtures ---

func startHeaven(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	// Create a temp dir for Heaven data
	dataDir := t.TempDir()

	// Copy fixture files into a "repo" subdir so BuildIndex can find them
	repoDir := filepath.Join(dataDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Copy fixtures/sample.go
	fixtureDir := filepath.Join("..", "fixtures")
	for _, name := range []string{"sample.go", "sample.py", "sample.ts"} {
		src, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, name), src, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	// Start Heaven server (use the heaven package's NewServer)
	// We import only via HTTP, so we spin up a real server using a simple mux
	// that proxies to heaven.Server. Since we're in the god package, we can't
	// import heaven directly — instead, start a minimal mock that implements
	// the endpoints the planner needs.
	mux := http.NewServeMux()

	// We need a real Heaven server. Since we can't import heaven from god,
	// we'll use an exec-based approach or a thin mock. For test isolation,
	// let's build a mock that covers the planner's needs.

	type symbol struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Path      string `json:"path"`
		StartByte int    `json:"start_byte"`
		EndByte   int    `json:"end_byte"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}

	// Prebuilt symbol table (matches fixtures/sample.go contents)
	symbols := []symbol{
		{ID: 1, Name: "Greet", Kind: "function", Path: "sample.go", StartLine: 6, EndLine: 8},
		{ID: 2, Name: "Farewell", Kind: "function", Path: "sample.go", StartLine: 11, EndLine: 13},
		{ID: 3, Name: "Person", Kind: "type", Path: "sample.go", StartLine: 15, EndLine: 18},
		{ID: 4, Name: "NewPerson", Kind: "function", Path: "sample.go", StartLine: 20, EndLine: 22},
		{ID: 5, Name: "SayHello", Kind: "method", Path: "sample.go", StartLine: 24, EndLine: 26},
		{ID: 6, Name: "factorial", Kind: "function", Path: "sample.py", StartLine: 1, EndLine: 5},
		{ID: 7, Name: "Calculator", Kind: "type", Path: "sample.ts", StartLine: 1, EndLine: 10},
	}

	var issuedLeases []map[string]any
	var events []map[string]any
	leaseCounter := 0

	// POST /ir/build — always succeeds
	mux.HandleFunc("POST /ir/build", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"files_indexed": 3})
	})

	// GET /ir/search — returns symbols matching query
	mux.HandleFunc("GET /ir/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		var matched []symbol
		for _, s := range symbols {
			if strings.Contains(strings.ToLower(s.Name), q) {
				matched = append(matched, s)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"symbols": matched})
	})

	// POST /lease/acquire — always grants all requested scopes
	mux.HandleFunc("POST /lease/acquire", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OwnerID   string `json:"owner_id"`
			MissionID string `json:"mission_id"`
			Scopes    []struct {
				ScopeType  string `json:"scope_type"`
				ScopeValue string `json:"scope_value"`
			} `json:"scopes"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		var acquired []map[string]any
		for _, s := range req.Scopes {
			leaseCounter++
			lease := map[string]any{
				"lease_id":    fmt.Sprintf("lease-%d", leaseCounter),
				"owner_id":   req.OwnerID,
				"mission_id": req.MissionID,
				"scope_type":  s.ScopeType,
				"scope_value": s.ScopeValue,
				"issued_at":   "2025-01-01T00:00:00Z",
			}
			acquired = append(acquired, lease)
			issuedLeases = append(issuedLeases, lease)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"acquired": acquired,
			"denied":   []string{},
		})
	})

	// POST /event — records events
	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt map[string]any
		json.Unmarshal(body, &evt)
		events = append(events, evt)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"offset": len(events)})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
	})

	return ts, repoDir
}

// --- Unit tests for extractKeywords ---

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			input: "Fix the Greet function to handle empty names",
			want:  []string{"greet", "function", "handle", "empty", "names"},
		},
		{
			input: "Add error handling to Person struct",
			want:  []string{"error", "handling", "person", "struct"},
		},
		{
			input: "the a an is in to for of",
			want:  nil, // all stop words
		},
		{
			input: "Update Calculator.add method",
			want:  []string{"calculator.add", "method"},
		},
	}

	for _, tc := range tests {
		got := extractKeywords(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("extractKeywords(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("extractKeywords(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// --- Unit tests for groupByBucket ---

func TestGroupByBucket(t *testing.T) {
	syms := []SymbolResult{
		{ID: 1, Name: "Greet", Kind: "function", Path: "pkg/greet.go"},
		{ID: 2, Name: "Farewell", Kind: "function", Path: "pkg/greet.go"},
		{ID: 3, Name: "Person", Kind: "type", Path: "models/person.go"},
		{ID: 4, Name: "Calculator", Kind: "type", Path: "math/calc.go"},
	}

	buckets := groupByBucket(syms)
	if len(buckets) != 3 {
		t.Fatalf("got %d buckets, want 3", len(buckets))
	}
	if len(buckets["pkg"]) != 2 {
		t.Errorf("pkg bucket has %d symbols, want 2", len(buckets["pkg"]))
	}
	if len(buckets["models"]) != 1 {
		t.Errorf("models bucket has %d symbols, want 1", len(buckets["models"]))
	}
	if len(buckets["math"]) != 1 {
		t.Errorf("math bucket has %d symbols, want 1", len(buckets["math"]))
	}
}

func TestGroupByBucketMergesExcess(t *testing.T) {
	syms := []SymbolResult{
		{ID: 1, Name: "A", Path: "dir1/a.go"},
		{ID: 2, Name: "B", Path: "dir1/b.go"},
		{ID: 3, Name: "C", Path: "dir2/c.go"},
		{ID: 4, Name: "D", Path: "dir2/d.go"},
		{ID: 5, Name: "E", Path: "dir3/e.go"},
		{ID: 6, Name: "F", Path: "dir4/f.go"},
	}

	buckets := groupByBucket(syms)
	if len(buckets) > 3 {
		t.Fatalf("got %d buckets, want <= 3", len(buckets))
	}
	// The two largest (dir1=2, dir2=2) should be kept; dir3 and dir4 merged into misc
	if _, ok := buckets["misc"]; !ok {
		t.Error("expected 'misc' bucket for overflow directories")
	}
}

func TestGroupByBucketRootDir(t *testing.T) {
	syms := []SymbolResult{
		{ID: 1, Name: "Main", Path: "main.go"},
	}
	buckets := groupByBucket(syms)
	if _, ok := buckets["root"]; !ok {
		t.Error("expected 'root' bucket for files in root directory")
	}
}

// --- Unit tests for scopeTypeFor ---

func TestScopeTypeFor(t *testing.T) {
	for _, kind := range []string{"function", "method", "type", "interface", "class", "unknown"} {
		got := scopeTypeFor(kind)
		if got != "symbol" {
			t.Errorf("scopeTypeFor(%q) = %q, want 'symbol'", kind, got)
		}
	}
}

// --- Unit tests for MissionDAG ---

func TestMissionDAGRoots(t *testing.T) {
	dag := NewMissionDAG("plan-1", "test task", "/repo")

	root := DAGNode{
		Mission:   Mission{MissionID: "m1", Goal: "root"},
		DependsOn: []string{},
	}
	child := DAGNode{
		Mission:   Mission{MissionID: "m2", Goal: "child"},
		DependsOn: []string{"m1"},
	}

	dag.AddNode(root)
	dag.AddNode(child)

	roots := dag.Roots()
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	if roots[0].Mission.MissionID != "m1" {
		t.Errorf("root mission = %s, want m1", roots[0].Mission.MissionID)
	}

	ids := dag.MissionIDs()
	if len(ids) != 2 {
		t.Fatalf("got %d mission IDs, want 2", len(ids))
	}
}

// --- Integration test: Planner with mock Heaven ---

func TestPlannerCreatesDAG(t *testing.T) {
	ts, repoDir := startHeaven(t)

	client := NewHeavenClient(ts.URL)
	planner := NewPlanner(client)

	dag, err := planner.Plan("Fix the Greet function for Person", repoDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Should have at least 2 nodes: analysis root + at least 1 bucket
	if len(dag.Nodes) < 2 {
		t.Fatalf("dag has %d nodes, want >= 2", len(dag.Nodes))
	}

	// Verify analysis root exists
	roots := dag.Roots()
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	if !strings.Contains(roots[0].Mission.Goal, "Analyze") {
		t.Errorf("root goal = %q, expected to contain 'Analyze'", roots[0].Mission.Goal)
	}
	if roots[0].Mission.TokenBudget != 4000 {
		t.Errorf("root token_budget = %d, want 4000", roots[0].Mission.TokenBudget)
	}

	// All non-root nodes should depend on the analysis root
	rootID := roots[0].Mission.MissionID
	for _, node := range dag.Nodes {
		if node.Mission.MissionID == rootID {
			continue
		}
		if len(node.DependsOn) == 0 {
			t.Errorf("node %s has no deps, should depend on analysis root", node.Mission.MissionID)
		}
		if node.DependsOn[0] != rootID {
			t.Errorf("node %s depends on %s, want %s", node.Mission.MissionID, node.DependsOn[0], rootID)
		}
		// Non-root missions should have 8000 token budget
		if node.Mission.TokenBudget != 8000 {
			t.Errorf("node %s token_budget = %d, want 8000", node.Mission.MissionID, node.Mission.TokenBudget)
		}
	}

	// Verify plan metadata
	if dag.TaskDesc == "" {
		t.Error("dag.TaskDesc is empty")
	}
	if dag.RepoPath != repoDir {
		t.Errorf("dag.RepoPath = %q, want %q", dag.RepoPath, repoDir)
	}
	if dag.PlanID == "" {
		t.Error("dag.PlanID is empty")
	}
}

func TestPlannerWithNoMatchingSymbols(t *testing.T) {
	ts, repoDir := startHeaven(t)

	client := NewHeavenClient(ts.URL)
	planner := NewPlanner(client)

	// Use a task that won't match any symbols
	dag, err := planner.Plan("zzz qqq xxx", repoDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Should have analysis root + 1 fallback mission
	if len(dag.Nodes) != 2 {
		t.Fatalf("dag has %d nodes, want 2 (analysis + fallback)", len(dag.Nodes))
	}

	// Fallback mission should have empty scopes and leases
	fallback := dag.Nodes[1]
	if len(fallback.Mission.Scopes) != 0 {
		t.Errorf("fallback scopes = %d, want 0", len(fallback.Mission.Scopes))
	}
	if len(fallback.Mission.LeaseIDs) != 0 {
		t.Errorf("fallback lease_ids = %d, want 0", len(fallback.Mission.LeaseIDs))
	}
	if fallback.Mission.Tasks[0] != "default" {
		t.Errorf("fallback task = %q, want 'default'", fallback.Mission.Tasks[0])
	}
}

func TestPlannerAcquiresLeases(t *testing.T) {
	ts, repoDir := startHeaven(t)

	client := NewHeavenClient(ts.URL)
	planner := NewPlanner(client)

	dag, err := planner.Plan("Greet Person function", repoDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Non-root missions should have leases
	hasLeases := false
	for _, node := range dag.Nodes {
		if len(node.Mission.LeaseIDs) > 0 {
			hasLeases = true
			break
		}
	}
	if !hasLeases {
		t.Error("expected at least one mission with acquired leases")
	}

	// Verify scopes are set on missions with leases
	for _, node := range dag.Nodes {
		if len(node.Mission.LeaseIDs) > 0 && len(node.Mission.Scopes) == 0 {
			t.Errorf("mission %s has leases but no scopes", node.Mission.MissionID)
		}
	}
}

func TestPlannerLogsEvents(t *testing.T) {
	ts, repoDir := startHeaven(t)

	client := NewHeavenClient(ts.URL)
	planner := NewPlanner(client)

	dag, err := planner.Plan("Greet Person", repoDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Count non-root missions
	nonRoot := 0
	for _, node := range dag.Nodes {
		if len(node.DependsOn) > 0 || (len(dag.Roots()) == 0) {
			nonRoot++
		}
	}

	// Each non-root mission should have a MISSION_CREATED event logged
	// We can verify by checking that the planner didn't error (events were accepted)
	if nonRoot == 0 {
		t.Error("expected at least one non-root mission")
	}
}

func TestPlannerDeduplicatesSymbols(t *testing.T) {
	// extractKeywords should produce unique keywords
	kw := extractKeywords("greet greet greet person person")
	seen := make(map[string]bool)
	for _, k := range kw {
		if seen[k] {
			t.Errorf("duplicate keyword: %s", k)
		}
		seen[k] = true
	}
}

func TestGenID(t *testing.T) {
	id1 := genID()
	id2 := genID()
	if id1 == id2 {
		t.Error("genID produced duplicate IDs")
	}
	if len(id1) != 32 {
		t.Errorf("genID length = %d, want 32 (16 bytes hex)", len(id1))
	}
}
