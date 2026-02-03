package heaven

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pfFixtureServer builds an index on fixtures and returns a test server.
func pfFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t) // reuse from irindex_test.go

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Build the IR index
	_, err = BuildIndex(context.Background(), s.irIndex, repoDir)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	return httptest.NewServer(s), repoDir
}

func postPF(t *testing.T, ts *httptest.Server, req PFRequest) PFResponse {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/pf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /pf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		t.Fatalf("POST /pf status=%d error=%s", resp.StatusCode, errResp["error"])
	}

	var pfResp PFResponse
	if err := json.NewDecoder(resp.Body).Decode(&pfResp); err != nil {
		t.Fatalf("decode PF response: %v", err)
	}
	return pfResp
}

func TestPFSymdefPrefetch(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "m1", Symbol: "Greet"},
	})

	if resp.Type != "response" {
		t.Fatalf("type = %q, want response", resp.Type)
	}

	// Should have at least 3 shards: symdef + callers (prefetch) + tests (stub)
	if len(resp.Shards) < 3 {
		t.Fatalf("PF_SYMDEF returned %d shards, want >= 3 (symdef + callers + tests)", len(resp.Shards))
	}

	// Check shard kinds
	kinds := make(map[string]bool)
	for _, s := range resp.Shards {
		kinds[s.Kind] = true
		if s.BlobID == "" {
			t.Fatalf("shard kind=%q has empty blob_id", s.Kind)
		}
	}
	if !kinds["symdef"] {
		t.Fatal("missing symdef shard")
	}
	if !kinds["callers"] {
		t.Fatal("missing callers shard (prefetch)")
	}
	if !kinds["tests"] {
		t.Fatal("missing tests shard (stub)")
	}

	// Prefetched flag should be true
	if !resp.Meta.Prefetched {
		t.Fatal("meta.prefetched should be true")
	}
}

func TestPFCallers(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_CALLERS",
		Args:    PFArgs{MissionID: "m1", Symbol: "Greet", TopK: 5},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_CALLERS returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "callers" {
		t.Fatalf("shard kind = %q, want callers", resp.Shards[0].Kind)
	}
}

func TestPFSlice(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	// Create a file to slice
	dir := t.TempDir()
	path := filepath.Join(dir, "code.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644)

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SLICE",
		Args:    PFArgs{MissionID: "m1", Path: path, StartLine: 2, N: 3},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_SLICE returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "slice" {
		t.Fatalf("shard kind = %q, want slice", resp.Shards[0].Kind)
	}
}

func TestPFSearch(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SEARCH",
		Args:    PFArgs{MissionID: "m1", Query: "Person", TopK: 10},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_SEARCH returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "search" {
		t.Fatalf("shard kind = %q, want search", resp.Shards[0].Kind)
	}
}

func TestPFStatus(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_STATUS",
		Args:    PFArgs{MissionID: "m1"},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_STATUS returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "status" {
		t.Fatalf("shard kind = %q, want status", resp.Shards[0].Kind)
	}
}

func TestPFTests(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_TESTS",
		Args:    PFArgs{MissionID: "m1", Symbol: "Greet"},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_TESTS returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "tests" {
		t.Fatalf("shard kind = %q, want tests", resp.Shards[0].Kind)
	}
}

func TestPFMetricsTracking(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	// First PF call
	resp1 := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "metrics-test", Symbol: "Greet"},
	})
	if resp1.Meta.PFCount != 1 {
		t.Fatalf("pf_count after 1st call = %d, want 1", resp1.Meta.PFCount)
	}
	if resp1.Meta.ShardBytes <= 0 {
		t.Fatal("shard_bytes should be > 0 after first call")
	}

	// Second PF call — same mission
	resp2 := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_CALLERS",
		Args:    PFArgs{MissionID: "metrics-test", Symbol: "Greet"},
	})
	if resp2.Meta.PFCount != 2 {
		t.Fatalf("pf_count after 2nd call = %d, want 2", resp2.Meta.PFCount)
	}
	if resp2.Meta.ShardBytes <= resp1.Meta.ShardBytes {
		t.Fatalf("shard_bytes should accumulate: %d <= %d", resp2.Meta.ShardBytes, resp1.Meta.ShardBytes)
	}
}

func TestPFMetricsLoggedToEvents(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	BuildIndex(context.Background(), s.irIndex, repoDir)
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Fire a PF
	postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "log-test", Symbol: "Greet"},
	})

	// Check events.log contains the PF event
	events, err := s.events.Tail(10)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	found := false
	for _, e := range events {
		if strings.Contains(string(e), `"type":"pf"`) && strings.Contains(string(e), `PF_SYMDEF`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("PF event not found in events.log")
	}
}

func TestPFInvalidCommand(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	body, _ := json.Marshal(PFRequest{
		Type:    "request",
		Command: "PF_INVALID",
		Args:    PFArgs{},
	})
	resp, err := http.Post(ts.URL+"/pf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /pf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPFInvalidType(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	body, _ := json.Marshal(PFRequest{
		Type:    "wrong",
		Command: "PF_STATUS",
		Args:    PFArgs{},
	})
	resp, err := http.Post(ts.URL+"/pf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /pf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPFBlobsRetrievable(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	BuildIndex(context.Background(), s.irIndex, repoDir)
	ts := httptest.NewServer(s)
	defer ts.Close()

	pfResp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "blob-test", Symbol: "Greet"},
	})

	// Every shard's blob_id should be retrievable via GET /blob/{id}
	for _, shard := range pfResp.Shards {
		resp, err := http.Get(ts.URL + "/blob/" + shard.BlobID)
		if err != nil {
			t.Fatalf("GET /blob/%s: %v", shard.BlobID, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("blob %s not found (kind=%s)", shard.BlobID, shard.Kind)
		}
		resp.Body.Close()
	}
}

func TestPFTestsStubContent(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	pfResp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_TESTS",
		Args:    PFArgs{MissionID: "stub-test", Symbol: "Greet"},
	})

	if len(pfResp.Shards) != 1 {
		t.Fatalf("PF_TESTS returned %d shards, want 1", len(pfResp.Shards))
	}
	shard := pfResp.Shards[0]
	if shard.Kind != "tests" {
		t.Fatalf("shard kind = %q, want tests", shard.Kind)
	}
	// Fetch blob content and verify it's a valid JSON array
	resp, err := http.Get(ts.URL + "/blob/" + shard.BlobID)
	if err != nil {
		t.Fatalf("GET blob: %v", err)
	}
	defer resp.Body.Close()
	var blobContent json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&blobContent); err != nil {
		t.Fatalf("decode blob response: %v", err)
	}
	// The blob is stored as JSON; content field contains the array
	var blobResp struct {
		Content string `json:"content"`
	}
	json.Unmarshal(blobContent, &blobResp)
	var arr []any
	if err := json.Unmarshal([]byte(blobResp.Content), &arr); err != nil {
		t.Fatalf("tests blob content is not a valid JSON array: %v", err)
	}
	// Stub returns empty array
	if len(arr) != 0 {
		t.Fatalf("stub tests should return empty array, got %d items", len(arr))
	}
	// Check stub marker in meta
	if shard.Meta["stub"] != true {
		t.Error("tests shard should have stub:true in meta")
	}
}

func TestPFTestsReturnsEmptyForUnknownSymbol(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	pfResp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_TESTS",
		Args:    PFArgs{MissionID: "unknown-sym", Symbol: "NonExistentSymbol12345"},
	})

	if len(pfResp.Shards) != 1 {
		t.Fatalf("PF_TESTS for unknown symbol returned %d shards, want 1", len(pfResp.Shards))
	}
	if pfResp.Shards[0].Kind != "tests" {
		t.Fatalf("shard kind = %q, want tests", pfResp.Shards[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// PF_PROMPT_* command tests
// ---------------------------------------------------------------------------

func storePromptForPFTest(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	raw := "# Spec\nBuild a calculator.\n## Constraints\nMust handle division by zero.\n## API\nGET /calc\n"
	resp, err := http.Post(ts.URL+"/prompt/store", "text/plain", strings.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /prompt/store: %v", err)
	}
	defer resp.Body.Close()
	var artifact struct {
		PromptID string `json:"prompt_id"`
	}
	json.NewDecoder(resp.Body).Decode(&artifact)
	if artifact.PromptID == "" {
		t.Fatal("failed to store prompt for PF test")
	}
	return artifact.PromptID
}

func TestPFPromptSection(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	promptID := storePromptForPFTest(t, ts)

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_PROMPT_SECTION",
		Args:    PFArgs{MissionID: "pf-prompt-1", PromptID: promptID, SectionIndex: 0},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_PROMPT_SECTION returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "prompt_section" {
		t.Fatalf("shard kind = %q, want prompt_section", resp.Shards[0].Kind)
	}
	if resp.Shards[0].Meta["section_index"] != float64(0) {
		t.Errorf("meta section_index = %v, want 0", resp.Shards[0].Meta["section_index"])
	}
}

func TestPFPromptSectionOutOfBounds(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	promptID := storePromptForPFTest(t, ts)

	body, _ := json.Marshal(PFRequest{
		Type:    "request",
		Command: "PF_PROMPT_SECTION",
		Args:    PFArgs{MissionID: "pf-prompt-2", PromptID: promptID, SectionIndex: 999},
	})
	resp, err := http.Post(ts.URL+"/pf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /pf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for out-of-bounds section", resp.StatusCode)
	}
}

func TestPFPromptSearch(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	promptID := storePromptForPFTest(t, ts)

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_PROMPT_SEARCH",
		Args:    PFArgs{MissionID: "pf-prompt-3", PromptID: promptID, Query: "division"},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_PROMPT_SEARCH returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "prompt_search" {
		t.Fatalf("shard kind = %q, want prompt_search", resp.Shards[0].Kind)
	}
	// Should have matches
	matches, ok := resp.Shards[0].Meta["matches"]
	if !ok {
		t.Fatal("meta missing 'matches' field")
	}
	if matches.(float64) < 1 {
		t.Error("expected at least 1 match for 'division'")
	}
}

func TestPFPromptSearchNoResults(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	promptID := storePromptForPFTest(t, ts)

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_PROMPT_SEARCH",
		Args:    PFArgs{MissionID: "pf-prompt-4", PromptID: promptID, Query: "xyznonexistent"},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_PROMPT_SEARCH returned %d shards, want 1", len(resp.Shards))
	}
	matches := resp.Shards[0].Meta["matches"]
	if matches.(float64) != 0 {
		t.Errorf("expected 0 matches for non-existent query, got %v", matches)
	}
}

func TestPFPromptSummary(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	promptID := storePromptForPFTest(t, ts)

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_PROMPT_SUMMARY",
		Args:    PFArgs{MissionID: "pf-prompt-5", PromptID: promptID},
	})

	if len(resp.Shards) != 1 {
		t.Fatalf("PF_PROMPT_SUMMARY returned %d shards, want 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "prompt_summary" {
		t.Fatalf("shard kind = %q, want prompt_summary", resp.Shards[0].Kind)
	}
	sectionCount, ok := resp.Shards[0].Meta["section_count"]
	if !ok {
		t.Fatal("meta missing 'section_count'")
	}
	if sectionCount.(float64) < 3 {
		t.Errorf("expected >= 3 sections in summary, got %v", sectionCount)
	}
}

// ---------------------------------------------------------------------------
// Context DCE: depth-aware PF_SYMDEF
// ---------------------------------------------------------------------------

func TestSymdefDepthSignature(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "depth-1", Symbol: "Greet", Depth: "signature"},
	})

	if len(resp.Shards) < 1 {
		t.Fatalf("PF_SYMDEF(signature) returned %d shards, want >= 1", len(resp.Shards))
	}
	if resp.Shards[0].Kind != "symdef" {
		t.Fatalf("shard kind = %q", resp.Shards[0].Kind)
	}
	if resp.Shards[0].Meta["depth"] != "signature" {
		t.Errorf("meta.depth = %v, want signature", resp.Shards[0].Meta["depth"])
	}
}

func TestSymdefDepthSummary(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "depth-2", Symbol: "Greet", Depth: "summary"},
	})

	if len(resp.Shards) < 1 {
		t.Fatalf("PF_SYMDEF(summary) returned %d shards, want >= 1", len(resp.Shards))
	}
	if resp.Shards[0].Meta["depth"] != "summary" {
		t.Errorf("meta.depth = %v, want summary", resp.Shards[0].Meta["depth"])
	}
}

func TestSymdefDepthFull(t *testing.T) {
	ts, _ := pfFixtureServer(t)
	defer ts.Close()

	resp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "depth-3", Symbol: "Greet", Depth: "full"},
	})

	if len(resp.Shards) < 1 {
		t.Fatalf("PF_SYMDEF(full) returned %d shards", len(resp.Shards))
	}
	// Full depth should still include prefetched callers
	if resp.Shards[0].Meta["depth"] != "full" {
		t.Errorf("meta.depth = %v, want full", resp.Shards[0].Meta["depth"])
	}
}

// ---------------------------------------------------------------------------
// PF Governor tests
// ---------------------------------------------------------------------------

func TestPFGovernorAccept(t *testing.T) {
	dataDir := t.TempDir()
	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	config := DefaultPFGovernorConfig()
	gov := NewPFGovernor(s.pf, config)

	resp, err := gov.Handle(PFRequest{
		Type:    "request",
		Command: "PF_STATUS",
		Args:    PFArgs{MissionID: "gov-1"},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp.Meta.BudgetRemaining == nil {
		t.Fatal("BudgetRemaining should be set")
	}
	if resp.Meta.BudgetRemaining.PFCallsLeft != config.MaxPFCalls-1 {
		t.Errorf("PFCallsLeft = %d, want %d", resp.Meta.BudgetRemaining.PFCallsLeft, config.MaxPFCalls-1)
	}
}

func TestPFGovernorRejectCalls(t *testing.T) {
	dataDir := t.TempDir()
	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	config := PFGovernorConfig{MaxPFCalls: 2, MaxShardBytes: 1 << 20}
	gov := NewPFGovernor(s.pf, config)

	req := PFRequest{
		Type:    "request",
		Command: "PF_STATUS",
		Args:    PFArgs{MissionID: "gov-reject"},
	}

	// First 2 calls should succeed
	for i := 0; i < 2; i++ {
		_, err := gov.Handle(req)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	// Third call should be rejected
	_, err = gov.Handle(req)
	if err == nil {
		t.Fatal("expected budget exceeded error on call 3")
	}
	if !strings.Contains(err.Error(), "budget exceeded") {
		t.Errorf("error = %q, want to contain 'budget exceeded'", err)
	}
}

func TestPFGovernorRejectBytes(t *testing.T) {
	dataDir := t.TempDir()
	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Very small byte limit
	config := PFGovernorConfig{MaxPFCalls: 100, MaxShardBytes: 1}
	gov := NewPFGovernor(s.pf, config)

	req := PFRequest{
		Type:    "request",
		Command: "PF_STATUS",
		Args:    PFArgs{MissionID: "gov-bytes"},
	}

	// First call succeeds (bytes are checked before the call)
	_, err = gov.Handle(req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call should be rejected because shard bytes from first call exceed limit
	_, err = gov.Handle(req)
	if err == nil {
		t.Fatal("expected budget exceeded error on byte limit")
	}
	if !strings.Contains(err.Error(), "budget exceeded") {
		t.Errorf("error = %q", err)
	}
}

// ---------------------------------------------------------------------------
// CDC/Delta PF tests
// ---------------------------------------------------------------------------

func TestDeltaTrackerFirstCall(t *testing.T) {
	dt := NewDeltaTracker()
	shards := []Shard{
		{Kind: "symdef", BlobID: "blob-1", Meta: map[string]any{}},
		{Kind: "callers", BlobID: "blob-2", Meta: map[string]any{}},
	}

	result, hits := dt.CheckAndUpdate("m1", "PF_SYMDEF", PFArgs{Symbol: "Foo"}, shards)
	if hits != 0 {
		t.Errorf("first call should have 0 delta hits, got %d", hits)
	}
	if len(result) != 2 {
		t.Errorf("result shards = %d, want 2", len(result))
	}
	// All shards should be original (not unchanged)
	for _, s := range result {
		if s.Kind == "unchanged" {
			t.Error("first call should not return unchanged shards")
		}
	}
}

func TestDeltaTrackerUnchanged(t *testing.T) {
	dt := NewDeltaTracker()
	shards := []Shard{
		{Kind: "symdef", BlobID: "blob-1", Meta: map[string]any{}},
	}
	args := PFArgs{Symbol: "Foo"}

	// First call: records version
	dt.CheckAndUpdate("m1", "PF_SYMDEF", args, shards)

	// Second call with same blob: should return unchanged
	result, hits := dt.CheckAndUpdate("m1", "PF_SYMDEF", args, shards)
	if hits != 1 {
		t.Errorf("second call should have 1 delta hit, got %d", hits)
	}
	if len(result) != 1 {
		t.Fatalf("result shards = %d, want 1", len(result))
	}
	if result[0].Kind != "unchanged" {
		t.Errorf("shard kind = %q, want unchanged", result[0].Kind)
	}
	if result[0].BlobID != "blob-1" {
		t.Errorf("unchanged shard should preserve blob_id")
	}
	if result[0].Meta["original_kind"] != "symdef" {
		t.Errorf("meta.original_kind = %v, want symdef", result[0].Meta["original_kind"])
	}
}

func TestDeltaTrackerChanged(t *testing.T) {
	dt := NewDeltaTracker()
	args := PFArgs{Symbol: "Foo"}

	// First call
	dt.CheckAndUpdate("m1", "PF_SYMDEF", args, []Shard{
		{Kind: "symdef", BlobID: "blob-v1", Meta: map[string]any{}},
	})

	// Second call with different blob: should return full shard
	result, hits := dt.CheckAndUpdate("m1", "PF_SYMDEF", args, []Shard{
		{Kind: "symdef", BlobID: "blob-v2", Meta: map[string]any{}},
	})
	if hits != 0 {
		t.Errorf("changed shard should have 0 delta hits, got %d", hits)
	}
	if result[0].Kind != "symdef" {
		t.Errorf("changed shard should be full, got kind=%q", result[0].Kind)
	}
	if result[0].BlobID != "blob-v2" {
		t.Errorf("changed shard should have new blob_id")
	}
}
