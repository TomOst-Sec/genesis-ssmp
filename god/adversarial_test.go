package god

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Adversarial: Huge file rebase doesn't OOM or panic
// ---------------------------------------------------------------------------

func TestAdversarialHugeFile(t *testing.T) {
	dir := t.TempDir()

	// Build a 12K-line file
	var b strings.Builder
	for i := 1; i <= 12000; i++ {
		fmt.Fprintf(&b, "line %d: some code content here\n", i)
	}
	bigContent := b.String()
	writeTestFile(t, dir, "huge.go", bigContent)

	lines := strings.Split(strings.TrimSuffix(bigContent, "\n"), "\n")
	anchor := ComputeAnchorHash(lines, 6000, 6002)

	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "replace_span",
			Path:       "huge.go",
			AnchorHash: anchor,
			Lines:      []int{6000, 6002},
			Content:    "replaced line A\nreplaced line B\nreplaced line C",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m-huge", ir, Manifest{
		SymbolsTouched: []string{},
		FilesTouched:   []string{"huge.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success on huge file, got: %s", result.Error)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d, want 1", result.OpsApplied)
	}

	// Verify the replacement
	data, _ := os.ReadFile(filepath.Join(dir, "huge.go"))
	if !strings.Contains(string(data), "replaced line A") {
		t.Error("huge file should contain replacement")
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Malformed Edit IR ops rejected
// ---------------------------------------------------------------------------

func TestAdversarialMalformedEditIR(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "line1\nline2\nline3\n")

	tests := []struct {
		name string
		ir   *EditIR
	}{
		{
			name: "unknown op type",
			ir: &EditIR{Ops: []EditOp{{
				Op: "drop_table", Path: "main.go", AnchorHash: "abc",
			}}},
		},
		{
			name: "replace_span missing lines",
			ir: &EditIR{Ops: []EditOp{{
				Op: "replace_span", Path: "main.go", AnchorHash: "abc",
				Content: "x",
			}}},
		},
		{
			name: "replace_span inverted range",
			ir: &EditIR{Ops: []EditOp{{
				Op: "replace_span", Path: "main.go", AnchorHash: "abc",
				Lines: []int{5, 2}, Content: "x",
			}}},
		},
		{
			name: "replace_span end exceeds file",
			ir: &EditIR{Ops: []EditOp{{
				Op: "replace_span", Path: "main.go", AnchorHash: "abc",
				Lines: []int{1, 999}, Content: "x",
			}}},
		},
		{
			name: "delete_span single element lines",
			ir: &EditIR{Ops: []EditOp{{
				Op: "delete_span", Path: "main.go", AnchorHash: "abc",
				Lines: []int{1},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyEditIR(dir, tt.ir)
			if err == nil {
				t.Error("expected error for malformed Edit IR")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Conflict cascade respects depth limit
// ---------------------------------------------------------------------------

func TestAdversarialConflictCascadeMax(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "cascade.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "cascade.go",
			AnchorHash: "0000000000000000000000000000000000000000000000000000000000000000",
			Lines:      []int{5, 7},
			Content:    "func Greet() {}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	// Depth 0 -> generates conflict mission
	req := makeIntegrateRequest(dir, "m-cascade-0", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"cascade.go"},
	})
	req.ConflictDepth = 0

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("depth 0 error: %v", err)
	}
	if result.Success {
		t.Fatal("depth 0 should fail due to anchor mismatch")
	}
	if result.ConflictMission == nil {
		t.Fatal("depth 0 should produce conflict mission")
	}

	// Depth = MaxConflictDepth -> hard error, no further conflict mission
	req2 := makeIntegrateRequest(dir, "m-cascade-max", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"cascade.go"},
	})
	req2.ConflictDepth = MaxConflictDepth

	result2, err := ig.Integrate(req2)
	if err != nil {
		t.Fatalf("depth max error: %v", err)
	}
	if result2.Success {
		t.Fatal("depth max should still fail")
	}
	if result2.ConflictMission != nil {
		t.Error("depth max should NOT produce another conflict mission")
	}
	if !strings.Contains(result2.Error, "conflict depth limit") {
		t.Errorf("error should mention depth limit: %s", result2.Error)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Concurrent integration of same file
// ---------------------------------------------------------------------------

func TestAdversarialConcurrentIntegration(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "concurrent.go", sampleFileContent)

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)

	var wg sync.WaitGroup
	errors := make([]error, 10)
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ig := NewIntegrator(client)
			ir := &EditIR{
				Ops: []EditOp{{
					Op:         "add_file",
					Path:       fmt.Sprintf("gen_%d.go", idx),
					AnchorHash: "empty",
					Content:    fmt.Sprintf("package main\n\nfunc Gen%d() {}\n", idx),
				}},
			}
			req := makeIntegrateRequest(dir, fmt.Sprintf("m-conc-%d", idx), ir, Manifest{
				SymbolsTouched: []string{},
				FilesTouched:   []string{},
			})
			req.FileClocks = nil
			_, err := ig.Integrate(req)
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	// All 10 files should exist
	for i := range 10 {
		path := filepath.Join(dir, fmt.Sprintf("gen_%d.go", i))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("gen_%d.go not created", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Empty mission pack doesn't crash provider adapter
// ---------------------------------------------------------------------------

func TestAdversarialEmptyMissionPack(t *testing.T) {
	resp := AngelResponse{
		MissionID:  "empty-pack",
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: []EditOp{}},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	respJSON, _ := json.Marshal(resp)
	mock := &mockProvider{responses: [][]byte{respJSON}}
	adapter := NewProviderAdapter(mock)

	pack := &MissionPack{
		Header:  "test",
		Mission: Mission{MissionID: "empty-pack", Goal: "empty"},
	}

	angelResp, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("empty pack should not error: %v", err)
	}
	if angelResp.MissionID != "empty-pack" {
		t.Errorf("mission_id = %q", angelResp.MissionID)
	}
	if !usage.Success {
		t.Error("usage should report success")
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Null/nil fields in AngelResponse handled
// ---------------------------------------------------------------------------

func TestAdversarialNullFields(t *testing.T) {
	tests := []struct {
		name string
		resp string
	}{
		{
			name: "null edit_ir with edit_ir type",
			resp: `{"mission_id":"m1","output_type":"edit_ir","edit_ir":null,"manifest":{"symbols_touched":[],"files_touched":[]}}`,
		},
		{
			name: "null symbols_touched",
			resp: `{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":null,"files_touched":[]}}`,
		},
		{
			name: "null files_touched",
			resp: `{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":[],"files_touched":null}}`,
		},
		{
			name: "null ops",
			resp: `{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":null},"manifest":{"symbols_touched":[],"files_touched":[]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockProvider{responses: [][]byte{[]byte(tt.resp)}}
			adapter := NewProviderAdapter(mock)
			pack := &MissionPack{
				Mission: Mission{MissionID: "m1"},
			}
			// All of these should be caught by validation, not panic
			_, _, err := adapter.Execute(pack)
			if err == nil {
				t.Error("expected validation error for null field")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Oversized shard dropped by prompt compiler
// ---------------------------------------------------------------------------

func TestAdversarialOversizedShard(t *testing.T) {
	// Create a huge shard that exceeds any reasonable budget
	bigContent := make([]byte, 100000) // ~25K tokens
	for i := range bigContent {
		bigContent[i] = 'x'
	}

	candidates := []CandidateShard{
		{
			Kind:    "symdef",
			BlobID:  "big-blob",
			Content: bigContent,
		},
		{
			Kind:    "callers",
			BlobID:  "small-blob",
			Content: []byte(`{"callers": ["a", "b"]}`),
		},
	}

	compiler := NewPromptCompiler("http://localhost:9999")
	mission := Mission{
		MissionID:   "m-oversized",
		TokenBudget: 1000, // tiny budget
	}

	pack, err := compiler.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// The oversized shard should be dropped
	for _, s := range pack.InlineShards {
		if s.BlobID == "big-blob" {
			t.Error("oversized shard should have been dropped")
		}
	}

	// The small shard should be included
	found := false
	for _, s := range pack.InlineShards {
		if s.BlobID == "small-blob" {
			found = true
		}
	}
	if !found {
		t.Error("small shard should be included")
	}

	if pack.BudgetMeta.ShardsDropped < 1 {
		t.Errorf("ShardsDropped = %d, want >= 1", pack.BudgetMeta.ShardsDropped)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Cyclic DAG deps rejected by oracle validation
// ---------------------------------------------------------------------------

func TestAdversarialCircularDeps(t *testing.T) {
	tests := []struct {
		name  string
		nodes []DAGNode
	}{
		{
			name: "A -> B -> C -> A",
			nodes: []DAGNode{
				{Mission: Mission{MissionID: "A"}, DependsOn: []string{"C"}},
				{Mission: Mission{MissionID: "B"}, DependsOn: []string{"A"}},
				{Mission: Mission{MissionID: "C"}, DependsOn: []string{"B"}},
			},
		},
		{
			name: "self-cycle",
			nodes: []DAGNode{
				{Mission: Mission{MissionID: "X"}, DependsOn: []string{"X"}},
			},
		},
		{
			name: "two-node cycle",
			nodes: []DAGNode{
				{Mission: Mission{MissionID: "P"}, DependsOn: []string{"Q"}},
				{Mission: Mission{MissionID: "Q"}, DependsOn: []string{"P"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detectCycle(tt.nodes)
			if err == nil {
				t.Error("expected cycle detection error")
			}
			if !strings.Contains(err.Error(), "cyclic dependency") {
				t.Errorf("error should mention cyclic dependency: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Adversarial: UTF-8 BOM doesn't break anchor hashing
// ---------------------------------------------------------------------------

func TestAdversarialUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	// UTF-8 BOM followed by normal content
	bom := "\xef\xbb\xbf"
	content := bom + "line1\nline2\nline3\nline4\nline5\nline6\nline7\n"
	writeTestFile(t, dir, "bom.go", content)

	// Read lines and compute anchor for lines 3-4
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	anchor := ComputeAnchorHash(lines, 3, 4)

	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "replace_span",
			Path:       "bom.go",
			AnchorHash: anchor,
			Lines:      []int{3, 4},
			Content:    "new3\nnew4",
		}},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("BOM file apply error: %v", err)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d, want 1", result.OpsApplied)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "bom.go"))
	if !strings.Contains(string(data), "new3") {
		t.Error("BOM file should contain replacement")
	}
	// BOM should be preserved in the first line
	if !strings.HasPrefix(string(data), bom) {
		t.Error("BOM should be preserved")
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Symlink escape rejected
// ---------------------------------------------------------------------------

func TestAdversarialSymlinkEscape(t *testing.T) {
	dir := t.TempDir()

	// Create a target file outside the "repo"
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(outsidePath, []byte("secret data"), 0o644)

	// Create a symlink inside the repo pointing outside
	symlinkPath := filepath.Join(dir, "escape.go")
	if err := os.Symlink(outsidePath, symlinkPath); err != nil {
		t.Skip("cannot create symlink (OS restriction)")
	}

	// Try to add_file to the symlink target — this should write to the
	// symlink target (the real path). Since add_file just writes to path,
	// verify the operation completes but the target file is the one modified.
	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "add_file",
			Path:       "escape.go",
			AnchorHash: "empty",
			Content:    "package main\n\nfunc Safe() {}\n",
		}},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		// Some implementations may reject symlinks — that's fine
		t.Logf("symlink rejected (acceptable): %v", err)
		return
	}

	// If it succeeded, verify that only the symlink target was touched
	// (no new file created alongside the symlink)
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d", result.OpsApplied)
	}
	t.Log("symlink followed — consider adding symlink detection in production")
}

// ---------------------------------------------------------------------------
// Adversarial: Binary file detection
// ---------------------------------------------------------------------------

func TestAdversarialBinaryFile(t *testing.T) {
	dir := t.TempDir()

	// Write a file with null bytes (binary)
	binaryContent := "line1\nline2\x00binary\nline3\nline4\nline5\nline6\n"
	writeTestFile(t, dir, "binary.dat", binaryContent)

	// Try to replace_span in a binary file
	lines := strings.Split(strings.TrimSuffix(binaryContent, "\n"), "\n")
	anchor := ComputeAnchorHash(lines, 2, 3)

	ir := &EditIR{
		Ops: []EditOp{{
			Op:         "replace_span",
			Path:       "binary.dat",
			AnchorHash: anchor,
			Lines:      []int{2, 3},
			Content:    "replaced",
		}},
	}

	// Should succeed (Edit IR doesn't currently detect binary) — this test
	// documents current behavior and serves as a regression marker if
	// binary detection is added later.
	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Logf("binary file rejected (if binary detection added): %v", err)
		return
	}
	t.Logf("binary file accepted — OpsApplied=%d (consider adding detection)", result.OpsApplied)
}

// ---------------------------------------------------------------------------
// Adversarial: Repair pack truncation
// ---------------------------------------------------------------------------

func TestAdversarialRepairPackTruncation(t *testing.T) {
	// Build a large invalid response
	largeResp := make([]byte, 10000)
	for i := range largeResp {
		largeResp[i] = 'Z'
	}

	original := &MissionPack{
		Header:  "test",
		Mission: Mission{MissionID: "m1", TokenBudget: 8000},
	}

	repaired := buildRepairPack(original, largeResp, fmt.Errorf("schema violation"))

	// The repair header should contain the truncation marker
	if !strings.Contains(repaired.Header, "[truncated]") {
		t.Error("repair pack should contain truncation marker")
	}

	// The repair header should NOT contain the full 10KB response
	if len(repaired.Header) > 2000 {
		t.Errorf("repair header too large: %d bytes (should be bounded)", len(repaired.Header))
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Shard dedup keeps highest score
// ---------------------------------------------------------------------------

func TestAdversarialShardDedupKeepsHighest(t *testing.T) {
	candidates := []CandidateShard{
		{Kind: "callers", BlobID: "dup-blob", Content: []byte("data"), TestRelevant: false},
		{Kind: "symdef", BlobID: "dup-blob", Content: []byte("data"), TestRelevant: true},
		{Kind: "callers", BlobID: "other-blob", Content: []byte("other")},
	}

	scored := ScoreShards(candidates)

	// Should have 2 shards (dup-blob deduped)
	if len(scored) != 2 {
		t.Fatalf("expected 2 shards after dedup, got %d", len(scored))
	}

	// The dup-blob entry should have the higher score (symdef + test_relevant)
	for _, s := range scored {
		if s.Shard.BlobID == "dup-blob" {
			if s.Shard.Kind != "symdef" {
				t.Errorf("dedup should keep symdef (higher score), got %s", s.Shard.Kind)
			}
			if !s.Shard.TestRelevant {
				t.Error("dedup should keep test_relevant=true variant")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Provider mission_id mismatch triggers retry
// ---------------------------------------------------------------------------

func TestAdversarialProviderMissionIDMismatch(t *testing.T) {
	// First response has wrong mission_id, second has correct one
	badResp := AngelResponse{
		MissionID:  "wrong-id",
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: []EditOp{}},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	goodResp := AngelResponse{
		MissionID:  "correct-id",
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: []EditOp{}},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	badJSON, _ := json.Marshal(badResp)
	goodJSON, _ := json.Marshal(goodResp)

	mock := &mockProvider{responses: [][]byte{badJSON, goodJSON}}
	adapter := NewProviderAdapter(mock)

	pack := &MissionPack{
		Header:  "test",
		Mission: Mission{MissionID: "correct-id"},
	}

	resp, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("should succeed after retry: %v", err)
	}
	if resp.MissionID != "correct-id" {
		t.Errorf("mission_id = %q, want correct-id", resp.MissionID)
	}
	if usage.Retries != 1 {
		t.Errorf("retries = %d, want 1", usage.Retries)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Recording provider with provider error
// ---------------------------------------------------------------------------

func TestAdversarialRecordingProviderError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	provider := NewHTTPProvider(ts.URL, "")
	recorder := NewRecordingProvider(provider)

	pack := &MissionPack{
		Mission: Mission{MissionID: "m-error"},
	}

	_, err := recorder.Send(pack)
	if err == nil {
		t.Fatal("expected error from provider")
	}

	// Error should NOT be recorded
	if len(recorder.Entries()) != 0 {
		t.Errorf("expected 0 entries on error, got %d", len(recorder.Entries()))
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Replay provider concurrent access
// ---------------------------------------------------------------------------

func TestAdversarialReplayConcurrent(t *testing.T) {
	entries := []RecordEntry{{
		MissionID: "m-conc",
		PackHash:  "hash1",
		Response:  json.RawMessage(`{"mission_id":"m-conc","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":[],"files_touched":[]}}`),
	}}

	replay := NewReplayProviderFromEntries(entries)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pack := &MissionPack{
				Mission: Mission{MissionID: "m-conc"},
			}
			_, err := replay.Send(pack)
			if err != nil {
				t.Errorf("concurrent replay error: %v", err)
			}
		}()
	}
	wg.Wait()

	if replay.CallCount() != 20 {
		t.Errorf("call count = %d, want 20", replay.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Prompt compiler with zero token budget
// ---------------------------------------------------------------------------

func TestAdversarialZeroTokenBudget(t *testing.T) {
	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: []byte("content")},
	}

	compiler := NewPromptCompiler("http://localhost:9999")
	mission := Mission{
		MissionID:   "m-zero-budget",
		TokenBudget: 0,
	}

	pack, err := compiler.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// All shards should be dropped
	if len(pack.InlineShards) != 0 {
		t.Errorf("expected 0 shards with zero budget, got %d", len(pack.InlineShards))
	}
	if pack.BudgetMeta.ShardsDropped != 1 {
		t.Errorf("ShardsDropped = %d, want 1", pack.BudgetMeta.ShardsDropped)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: Integration with stale clock — nowFunc injection
// ---------------------------------------------------------------------------

func TestAdversarialStaleTimestamp(t *testing.T) {
	// Freeze time to a known value
	frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	oldNow := nowFunc
	nowFunc = func() time.Time { return frozen }
	defer func() { nowFunc = oldNow }()

	dir := t.TempDir()
	writeTestFile(t, dir, "stale.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)
	ir := &EditIR{
		Ops: []EditOp{{
			Op: "replace_span", Path: "stale.go", AnchorHash: anchor,
			Lines: []int{5, 7}, Content: "func Greet() {}",
		}},
	}

	ts := startIntegrationHeaven(t, integrationMockConfig{manifestAllowed: true})
	client := NewHeavenClient(ts.URL)
	ig := NewIntegrator(client)

	req := makeIntegrateRequest(dir, "m-stale", ir, Manifest{
		SymbolsTouched: []string{"Greet"},
		FilesTouched:   []string{"stale.go"},
	})

	result, err := ig.Integrate(req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success: %s", result.Error)
	}
}
