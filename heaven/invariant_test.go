package heaven

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// I2: PF response size must be bounded (<200KB)
// ---------------------------------------------------------------------------

func TestPFResponseSizeBound(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	BuildIndex(context.Background(), s.irIndex, repoDir)
	ts := httptest.NewServer(s)
	defer ts.Close()

	commands := []string{"PF_SYMDEF", "PF_CALLERS", "PF_SEARCH", "PF_STATUS", "PF_TESTS"}
	args := []PFArgs{
		{MissionID: "bound-1", Symbol: "Greet"},
		{MissionID: "bound-2", Symbol: "Greet", TopK: 100},
		{MissionID: "bound-3", Query: "Person", TopK: 100},
		{MissionID: "bound-4"},
		{MissionID: "bound-5", Symbol: "Greet"},
	}

	const maxResponseBytes = 200 * 1024 // 200KB

	for i, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			body, _ := json.Marshal(PFRequest{
				Type:    "request",
				Command: cmd,
				Args:    args[i],
			})

			resp, err := http.Post(ts.URL+"/pf", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("POST /pf: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return // invalid commands expected to fail, not a size issue
			}

			var pfResp PFResponse
			if err := json.NewDecoder(resp.Body).Decode(&pfResp); err != nil {
				t.Fatalf("decode: %v", err)
			}

			respJSON, _ := json.Marshal(pfResp)
			if len(respJSON) > maxResponseBytes {
				t.Errorf("%s response is %d bytes > %d bytes (200KB limit)",
					cmd, len(respJSON), maxResponseBytes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// I2: PF shard content should not exceed 10K lines
// ---------------------------------------------------------------------------

func TestPFShardLinesBound(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	BuildIndex(context.Background(), s.irIndex, repoDir)
	ts := httptest.NewServer(s)
	defer ts.Close()

	const maxShardLines = 10000

	// PF_SYMDEF should return bounded shards
	pfResp := postPF(t, ts, PFRequest{
		Type:    "request",
		Command: "PF_SYMDEF",
		Args:    PFArgs{MissionID: "lines-1", Symbol: "Greet"},
	})

	for _, shard := range pfResp.Shards {
		// Fetch blob content
		resp, err := http.Get(ts.URL + "/blob/" + shard.BlobID)
		if err != nil {
			t.Fatalf("GET blob %s: %v", shard.BlobID, err)
		}
		var blobResp GetBlobResponse
		json.NewDecoder(resp.Body).Decode(&blobResp)
		resp.Body.Close()

		lineCount := 1
		for _, c := range blobResp.Content {
			if c == '\n' {
				lineCount++
			}
		}
		if lineCount > maxShardLines {
			t.Errorf("shard %s (kind=%s) has %d lines > %d limit",
				shard.BlobID, shard.Kind, lineCount, maxShardLines)
		}
	}
}

// ---------------------------------------------------------------------------
// Blob binary content roundtrip (including null bytes)
// ---------------------------------------------------------------------------

func TestBlobBinaryRoundtrip(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	// Binary content with null bytes
	binary := make([]byte, 256)
	for i := range binary {
		binary[i] = byte(i)
	}

	id, err := bs.Put(binary)
	if err != nil {
		t.Fatalf("Put binary: %v", err)
	}

	got, err := bs.Get(id)
	if err != nil {
		t.Fatalf("Get binary: %v", err)
	}
	if len(got) != len(binary) {
		t.Fatalf("size mismatch: got %d, want %d", len(got), len(binary))
	}
	for i := range binary {
		if got[i] != binary[i] {
			t.Fatalf("byte mismatch at offset %d: got %d, want %d", i, got[i], binary[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Blob empty content rejected
// ---------------------------------------------------------------------------

func TestBlobEmptyContentRejected(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/blob", "text/plain", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("POST /blob: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for empty blob body")
	}
}

// ---------------------------------------------------------------------------
// Events invalid JSON rejected at HTTP level
// ---------------------------------------------------------------------------

func TestEventEndpointRejectsInvalidJSON(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/event", "application/json",
		bytes.NewReader([]byte("{{not json")))
	if err != nil {
		t.Fatalf("POST /event: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for invalid JSON event")
	}
}

// ---------------------------------------------------------------------------
// PF_SYMDEF for non-existent symbol returns valid response (no crash)
// ---------------------------------------------------------------------------

func TestPFSymdefNonExistentSymbol(t *testing.T) {
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
		Args:    PFArgs{MissionID: "missing-1", Symbol: "NonExistentSymbol99999"},
	})

	// Should return a valid response (possibly with empty/stub shards)
	if pfResp.Type != "response" {
		t.Fatalf("type = %q, want response", pfResp.Type)
	}
}

// ---------------------------------------------------------------------------
// Lease + FileClock persistence across HeavenEnv.Restart
// ---------------------------------------------------------------------------

func TestFullStatePersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// Server 1: create state
	s1, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(1): %v", err)
	}
	ts1 := httptest.NewServer(s1)

	// Store a blob
	blobResp := decodeJSON[PutBlobResponse](t,
		mustPost(t, ts1.URL+"/blob", "text/plain", "persistent content"))

	// Acquire a lease
	mustPost(t, ts1.URL+"/lease/acquire", "application/json",
		`{"owner_id":"persist-owner","mission_id":"pm1","scopes":[{"scope_type":"symbol","scope_value":"PersistSym"}]}`).Body.Close()

	// Increment file clock
	mustPost(t, ts1.URL+"/file-clock/inc", "application/json",
		`{"paths":["persist.go"]}`).Body.Close()
	mustPost(t, ts1.URL+"/file-clock/inc", "application/json",
		`{"paths":["persist.go"]}`).Body.Close()

	// Append an event
	mustPost(t, ts1.URL+"/event", "application/json",
		`{"type":"test_persist","data":"survive_restart"}`).Body.Close()

	ts1.Close()

	// Server 2: verify everything survived
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(2): %v", err)
	}
	ts2 := httptest.NewServer(s2)
	defer ts2.Close()

	// Blob intact
	getResp, err := http.Get(ts2.URL + "/blob/" + blobResp.BlobID)
	if err != nil {
		t.Fatalf("GET blob after restart: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("blob %s lost after restart", blobResp.BlobID)
	}
	getResp.Body.Close()

	// Lease intact
	leaseList := decodeJSON[LeaseListResponse](t, mustGet(t, ts2.URL+"/lease/list"))
	if len(leaseList.Leases) != 1 {
		t.Fatalf("leases after restart: %d, want 1", len(leaseList.Leases))
	}
	if leaseList.Leases[0].ScopeValue != "PersistSym" {
		t.Errorf("lease scope = %q, want PersistSym", leaseList.Leases[0].ScopeValue)
	}

	// File clock intact
	clockResult := decodeJSON[FileClockGetResponse](t,
		mustPost(t, ts2.URL+"/file-clock/get", "application/json",
			`{"paths":["persist.go"]}`))
	if clockResult.Clocks["persist.go"] != 2 {
		t.Fatalf("file clock after restart: %d, want 2", clockResult.Clocks["persist.go"])
	}

	// Events intact
	tailResult := decodeJSON[TailEventsResponse](t, mustGet(t, ts2.URL+"/events/tail?n=100"))
	found := false
	for _, evt := range tailResult.Events {
		if bytes.Contains([]byte(evt), []byte("survive_restart")) {
			found = true
			break
		}
	}
	if !found {
		t.Error("persist event not found after restart")
	}

	// state_rev reconstructed
	status := decodeJSON[StatusResponse](t, mustGet(t, ts2.URL+"/status"))
	if status.StateRev < 1 {
		t.Errorf("state_rev after restart = %d, want >= 1", status.StateRev)
	}
}
