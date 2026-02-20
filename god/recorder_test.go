package god

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndReplay(t *testing.T) {
	// Set up a mock provider
	mockResp := AngelResponse{
		MissionID:  "test-m1",
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: []EditOp{}},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	respJSON, _ := json.Marshal(mockResp)

	mock := &mockProvider{responses: [][]byte{respJSON}}
	recorder := NewRecordingProvider(mock)

	// Record a call
	pack := &MissionPack{
		Header:  "test header",
		Mission: Mission{MissionID: "test-m1", Goal: "test"},
	}
	resp, err := recorder.Send(pack)
	if err != nil {
		t.Fatalf("record send: %v", err)
	}
	if string(resp) != string(respJSON) {
		t.Fatalf("response mismatch")
	}

	// Check entries
	entries := recorder.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].MissionID != "test-m1" {
		t.Errorf("entry mission_id = %q, want test-m1", entries[0].MissionID)
	}
	if entries[0].PackHash == "" {
		t.Error("entry pack_hash is empty")
	}

	// Save to file
	tmpFile := filepath.Join(t.TempDir(), "recording.jsonl")
	if err := recorder.SaveTo(tmpFile); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify file exists and is non-empty
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("recording file is empty")
	}

	// Load and replay
	replay, err := NewReplayProvider(tmpFile)
	if err != nil {
		t.Fatalf("load replay: %v", err)
	}

	// Same pack should produce same response
	replayResp, err := replay.Send(pack)
	if err != nil {
		t.Fatalf("replay send: %v", err)
	}
	if string(replayResp) != string(respJSON) {
		t.Fatalf("replay response mismatch")
	}
	if replay.CallCount() != 1 {
		t.Errorf("replay call count = %d, want 1", replay.CallCount())
	}
}

func TestReplayMissionIDFallback(t *testing.T) {
	// Create entries with known mission ID
	entries := []RecordEntry{{
		MissionID: "fallback-m1",
		PackHash:  "deadbeef",
		Response:  json.RawMessage(`{"mission_id":"fallback-m1","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":[],"files_touched":[]}}`),
	}}

	replay := NewReplayProviderFromEntries(entries)

	// Pack with different hash but same mission_id
	pack := &MissionPack{
		Header:  "different header causing different hash",
		Mission: Mission{MissionID: "fallback-m1"},
	}

	resp, err := replay.Send(pack)
	if err != nil {
		t.Fatalf("fallback send: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("empty response from fallback")
	}
}

func TestReplayNoMatch(t *testing.T) {
	replay := NewReplayProviderFromEntries(nil)

	pack := &MissionPack{
		Mission: Mission{MissionID: "nonexistent"},
	}

	_, err := replay.Send(pack)
	if err == nil {
		t.Fatal("expected error for unrecorded pack")
	}
}

func TestRecordingProviderMultipleCalls(t *testing.T) {
	respJSON := []byte(`{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":[],"files_touched":[]}}`)
	mock := &mockProvider{responses: [][]byte{respJSON}}
	recorder := NewRecordingProvider(mock)

	for i := 0; i < 5; i++ {
		pack := &MissionPack{
			Mission: Mission{MissionID: "m1", Goal: "test"},
		}
		recorder.Send(pack)
	}

	if len(recorder.Entries()) != 5 {
		t.Errorf("expected 5 entries, got %d", len(recorder.Entries()))
	}
}

func TestReplayByTurnNumber(t *testing.T) {
	// Simulate a 3-turn session with distinct responses per turn
	entries := []RecordEntry{
		{
			MissionID:  "multi-turn",
			TurnNumber: 0,
			Phase:      "understand",
			PackHash:   "hash0",
			Response:   json.RawMessage(`{"turn":0}`),
			TokensIn:   100,
		},
		{
			MissionID:  "multi-turn",
			TurnNumber: 1,
			Phase:      "plan",
			PackHash:   "hash1",
			Response:   json.RawMessage(`{"turn":1}`),
			TokensIn:   200,
		},
		{
			MissionID:  "multi-turn",
			TurnNumber: 2,
			Phase:      "execute",
			PackHash:   "hash2",
			Response:   json.RawMessage(`{"turn":2}`),
			TokensIn:   300,
		},
	}

	replay := NewReplayProviderFromEntries(entries)

	// Each Send should return the response for the corresponding turn
	for i := 0; i < 3; i++ {
		pack := &MissionPack{
			Header:  "different-each-time", // hash won't match — forces turn-based lookup
			Mission: Mission{MissionID: "multi-turn"},
		}
		resp, err := replay.Send(pack)
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		expected := fmt.Sprintf(`{"turn":%d}`, i)
		if string(resp) != expected {
			t.Errorf("turn %d: got %s, want %s", i, string(resp), expected)
		}
	}

	if replay.CallCount() != 3 {
		t.Errorf("call count = %d, want 3", replay.CallCount())
	}
}

func TestReplayBackwardCompat(t *testing.T) {
	// Old-format entries: no TurnNumber (defaults to 0), no Phase, no TokensIn
	entries := []RecordEntry{
		{
			MissionID: "old-m1",
			PackHash:  "oldhash1",
			Response:  json.RawMessage(`{"old":"response1"}`),
		},
		{
			MissionID: "old-m2",
			PackHash:  "oldhash2",
			Response:  json.RawMessage(`{"old":"response2"}`),
		},
	}

	replay := NewReplayProviderFromEntries(entries)

	// Should still work via turn-based key (turn 0 is the default)
	pack1 := &MissionPack{Mission: Mission{MissionID: "old-m1"}}
	resp1, err := replay.Send(pack1)
	if err != nil {
		t.Fatalf("old-m1: %v", err)
	}
	if string(resp1) != `{"old":"response1"}` {
		t.Errorf("old-m1: got %s", string(resp1))
	}

	// Second mission
	pack2 := &MissionPack{Mission: Mission{MissionID: "old-m2"}}
	resp2, err := replay.Send(pack2)
	if err != nil {
		t.Fatalf("old-m2: %v", err)
	}
	if string(resp2) != `{"old":"response2"}` {
		t.Errorf("old-m2: got %s", string(resp2))
	}

	// No drift violations since no TokensIn recorded
	violations := replay.ValidateReplay(0.1)
	if len(violations) != 0 {
		t.Errorf("expected 0 drift violations for old entries, got %d", len(violations))
	}
}

func TestReplayTokenDriftValidation(t *testing.T) {
	// Record entries with known token counts
	entries := []RecordEntry{
		{
			MissionID:  "drift-m1",
			TurnNumber: 0,
			PackHash:   "drifthash",
			Response:   json.RawMessage(`{"ok":true}`),
			TokensIn:   100,
		},
	}

	replay := NewReplayProviderFromEntries(entries)

	// Send a pack — the actual token count will differ from recorded 100
	pack := &MissionPack{
		Mission: Mission{MissionID: "drift-m1"},
	}
	_, err := replay.Send(pack)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// There should be drift recorded (actual tokens != 100)
	allDrift := replay.ValidateReplay(0.0) // tolerance 0 = flag everything
	if len(allDrift) == 0 {
		t.Fatal("expected drift to be recorded")
	}

	d := allDrift[0]
	if d.MissionID != "drift-m1" {
		t.Errorf("drift mission_id = %q", d.MissionID)
	}
	if d.RecordedIn != 100 {
		t.Errorf("drift recorded_in = %d, want 100", d.RecordedIn)
	}
	if d.DriftPct <= 0 {
		t.Errorf("drift_pct should be > 0, got %f", d.DriftPct)
	}

	// With a very high tolerance, no violations
	highTol := replay.ValidateReplay(100.0)
	if len(highTol) != 0 {
		t.Errorf("expected 0 violations at 100%% tolerance, got %d", len(highTol))
	}
}