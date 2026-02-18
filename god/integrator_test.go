package god

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock Heaven server for Integrator tests
// ---------------------------------------------------------------------------

type integrationMockConfig struct {
	manifestAllowed bool
	manifestReason  string
	missingLeases   []string
	clockDrift      []string
	fileClocks      map[string]int64
}

func startIntegrationHeaven(t *testing.T, cfg integrationMockConfig) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /validate-manifest", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"allowed": cfg.manifestAllowed,
		}
		if !cfg.manifestAllowed {
			resp["reason"] = cfg.manifestReason
			leases := cfg.missingLeases
			if leases == nil {
				leases = []string{}
			}
			drift := cfg.clockDrift
			if drift == nil {
				drift = []string{}
			}
			resp["missing_leases"] = leases
			resp["clock_drift"] = drift
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /file-clock/get", func(w http.ResponseWriter, r *http.Request) {
		clocks := cfg.fileClocks
		if clocks == nil {
			clocks = map[string]int64{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"clocks": clocks})
	})

	mux.HandleFunc("POST /file-clock/inc", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Paths []string `json:"paths"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		clocks := make(map[string]int64)
		for _, p := range req.Paths {
			if v, ok := cfg.fileClocks[p]; ok {
				clocks[p] = v + 1
			} else {
				clocks[p] = 1
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"clocks": clocks})
	})

	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"offset": 1})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func makeIntegrateRequest(repoRoot, missionID string, ir *EditIR, manifest Manifest) IntegrateRequest {
	return IntegrateRequest{
		OwnerID:  "god-test",
		RepoRoot: repoRoot,
		Response: &AngelResponse{
			MissionID:  missionID,
			OutputType: "edit_ir",
			EditIR:     ir,
			Manifest:   manifest,
		},
		Mission: Mission{
			MissionID:   missionID,
			Goal:        "test",
			BaseRev:     "abc123",
			Scopes:      []Scope{},
			LeaseIDs:    []string{},
			Tasks:       []string{"task1"},
			TokenBudget: 8000,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		FileClocks: map[string]int64{"main.go": 0},
	}
}

// ---------------------------------------------------------------------------
// Successful integration tests
// ---------------------------------------------------------------------------

func TestIntegrateSuccess(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)
	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "replace_span",
			Path:       "main.go",
			AnchorHash: anchor,
			Lines:      []int{5, 7},
			Content:    "func Greet(p Person) string {\n\treturn \"Hello, \" + p.Name\n}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d, want 1", result.OpsApplied)
	}
	if len(result.FilesModified) != 1 || result.FilesModified[0] != "main.go" {
		t.Errorf("FilesModified = %v", result.FilesModified)
	}
	if len(result.Diffs) == 0 {
		t.Error("expected diffs to be generated")
	}

	// Verify file was actually modified
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(content), "func Greet(p Person)") {
		t.Error("file should contain updated code")
	}
}

func TestIntegrateAddFile(t *testing.T) {
	dir := t.TempDir()

	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "add_file",
			Path:       "util.go",
			AnchorHash: "empty",
			Content:    "package main\n\nfunc Helper() {}\n",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{},
		FilesTouched:   []string{},
	})
	req.FileClocks = nil

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}
	if len(result.FilesCreated) != 1 {
		t.Errorf("FilesCreated = %v", result.FilesCreated)
	}
}

// ---------------------------------------------------------------------------
// Manifest validation failure
// ---------------------------------------------------------------------------

func TestIntegrateManifestDenied(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)
	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "main.go", AnchorHash: anchor,
			Lines: []int{5, 7}, Content: "replaced",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{
		manifestAllowed: false,
		manifestReason:  "missing leases",
		missingLeases:   []string{"symbol:Greet"},
	})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure due to missing leases")
	}
	if !strings.Contains(result.Error, "manifest validation failed") {
		t.Errorf("error should mention validation: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Clock drift → rebase → success
// ---------------------------------------------------------------------------

func TestIntegrateClockDriftRebaseSuccess(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	// Compute correct anchor for current file state
	anchor := anchorFor(t, sampleFileContent, 5, 7)
	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "main.go", AnchorHash: anchor,
			Lines: []int{5, 7}, Content: "func Greet(p Person) string {\n\treturn \"Hello, \" + p.Name\n}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{
		manifestAllowed: false,
		manifestReason:  "file clock drift",
		clockDrift:      []string{"main.go: expected=0 actual=1"},
	})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Rebase should succeed because the file hasn't actually changed on disk
	if !result.Success {
		t.Fatalf("expected success after rebase, got: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Apply failure → conflict mission
// ---------------------------------------------------------------------------

func TestIntegrateConflictGeneratesMission(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	// Use a bad anchor hash to force apply failure
	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "main.go",
			AnchorHash: "0000000000000000000000000000000000000000000000000000000000000000",
			Lines:      []int{5, 7},
			Content:    "func Greet(p Person) {}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure due to anchor mismatch")
	}
	if result.ConflictMission == nil {
		t.Fatal("expected conflict mission to be generated")
	}

	cm := result.ConflictMission
	if cm.Mission.MissionID == "" {
		t.Error("conflict mission should have an ID")
	}
	if !strings.Contains(cm.Mission.Goal, "[CONFLICT]") {
		t.Errorf("conflict mission goal should contain [CONFLICT], got: %s", cm.Mission.Goal)
	}
	if cm.Mission.TokenBudget != 4000 {
		t.Errorf("conflict mission budget = %d, want 4000", cm.Mission.TokenBudget)
	}
	if len(cm.ShardRequests) == 0 {
		t.Error("conflict mission should have shard requests for the conflict region")
	}

	// Verify original file wasn't modified
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(content) != sampleFileContent {
		t.Error("original file should not be modified after conflict")
	}
}

func TestIntegrateConflictMissionHasSliceNeed(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "main.go",
			AnchorHash: "bad_anchor_hash_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Lines:      []int{5, 7},
			Content:    "func Greet() {}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ConflictMission == nil {
		t.Fatal("expected conflict mission")
	}

	// Check that the shard request is a PF_SLICE for the conflict region
	found := false
	for _, sr := range result.ConflictMission.ShardRequests {
		if sr.Command == "PF_SLICE" && sr.Args.Path == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("conflict mission should include PF_SLICE for conflict file")
	}
}

// ---------------------------------------------------------------------------
// Clock drift + rebase failure → conflict
// ---------------------------------------------------------------------------

func TestIntegrateClockDriftRebaseFailConflict(t *testing.T) {
	dir := t.TempDir()
	// Write a file that doesn't match the expected line range (only 2 lines)
	writeTestFile(t, dir, "main.go", "line1\nline2\n")

	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "main.go", AnchorHash: "old_anchor",
			Lines: []int{5, 10}, Content: "replaced",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{
		manifestAllowed: false,
		manifestReason:  "file clock drift",
		clockDrift:      []string{"main.go: expected=0 actual=2"},
	})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ConflictMission == nil {
		t.Fatal("expected conflict mission after rebase failure")
	}
}

// ---------------------------------------------------------------------------
// Unsupported output type
// ---------------------------------------------------------------------------

func TestIntegrateUnsupportedOutputType(t *testing.T) {
	dir := t.TempDir()
	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := IntegrateRequest{
		OwnerID:  "god-test",
		RepoRoot: dir,
		Response: &AngelResponse{
			MissionID:  "m1",
			OutputType: "diff_fallback",
			Diff:       "some diff",
			Manifest: Manifest{
				SymbolsTouched: []string{},
				FilesTouched:   []string{},
			},
		},
		Mission: Mission{
			MissionID: "m1",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}

	_, err := ig.Integrate(req)
	if err == nil {
		t.Fatal("expected error for unsupported output type")
	}
	if !strings.Contains(err.Error(), "diff_fallback") {
		t.Errorf("error should mention output type: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Multiple ops integration
// ---------------------------------------------------------------------------

func TestIntegrateMultipleOps(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)
	ir := &EditIR{
		Ops: []EditOp{
			{
				Op: "replace_span", Path: "main.go", AnchorHash: anchor,
				Lines: []int{5, 7}, Content: "func Greet(p Person) string {\n\treturn p.Name\n}",
			},
			{
				Op: "add_file", Path: "types.go", AnchorHash: "empty",
				Content: "package main\n\ntype Person struct{ Name string }\n",
			},
		},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m1", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"main.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success: %s", result.Error)
	}
	if result.OpsApplied != 2 {
		t.Errorf("OpsApplied = %d, want 2", result.OpsApplied)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func TestIntegrateRebasePathAppliesCorrectly(t *testing.T) {
	dir := t.TempDir()
	// Write initial file
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\n"
	writeTestFile(t, dir, "rebased.go", content)

	anchor := anchorFor(t, content, 3, 4)
	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "rebased.go",
				AnchorHash: anchor,
				Lines:      []int{3, 4},
				Content:    "newline3\nnewline4",
			},
		},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m-rebase", ir, Manifest{
		SymbolsTouched: []string{},
		FilesTouched:   []string{"rebased.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}

	// Verify file was modified
	data, _ := os.ReadFile(filepath.Join(dir, "rebased.go"))
	if !strings.Contains(string(data), "newline3") {
		t.Error("rebased file should contain newline3")
	}
}

func TestIntegrateConflictHunkSizeBounded(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "conflict.go", sampleFileContent)

	// Use bad anchor hash to force conflict
	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "conflict.go",
				AnchorHash: "bad-anchor-hash",
				Lines:      []int{3, 5},
				Content:    "new content",
			},
		},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m-conflict", ir, Manifest{
		SymbolsTouched: []string{},
		FilesTouched:   []string{"conflict.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure due to anchor mismatch")
	}
	if result.ConflictMission == nil {
		t.Fatal("expected conflict mission to be generated")
	}
	// Conflict mission should have reduced token budget
	if result.ConflictMission.Mission.TokenBudget > 8000 {
		t.Errorf("conflict mission budget = %d, should be bounded", result.ConflictMission.Mission.TokenBudget)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
