package god

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// RecordEntry is a single recorded provider interaction.
type RecordEntry struct {
	MissionID    string          `json:"mission_id"`
	TurnNumber   int             `json:"turn_number"`
	Phase        string          `json:"phase,omitempty"`
	PackHash     string          `json:"pack_hash"`
	RequestBytes int             `json:"request_bytes"`
	Response     json.RawMessage `json:"response"`
	TokensIn     int             `json:"tokens_in,omitempty"`
	TokensOut    int             `json:"tokens_out,omitempty"`
	Timestamp    string          `json:"timestamp"`
}

// RecordingProvider wraps a Provider and records all Send() calls to a JSONL file.
type RecordingProvider struct {
	inner       Provider
	entries     []RecordEntry
	turnCounter map[string]int // mission_id -> next turn number
	mu          sync.Mutex
}

// NewRecordingProvider wraps a provider with recording.
func NewRecordingProvider(inner Provider) *RecordingProvider {
	return &RecordingProvider{
		inner:       inner,
		turnCounter: make(map[string]int),
	}
}

// Send calls the inner provider and records the interaction.
func (rp *RecordingProvider) Send(pack *MissionPack) ([]byte, error) {
	packJSON, _ := json.Marshal(pack)
	packHash := hashBytes(packJSON)

	rp.mu.Lock()
	turn := rp.turnCounter[pack.Mission.MissionID]
	rp.turnCounter[pack.Mission.MissionID] = turn + 1
	rp.mu.Unlock()

	resp, err := rp.inner.Send(pack)
	if err != nil {
		return nil, err
	}

	entry := RecordEntry{
		MissionID:    pack.Mission.MissionID,
		TurnNumber:   turn,
		Phase:        pack.Phase,
		PackHash:     packHash,
		RequestBytes: len(packJSON),
		Response:     json.RawMessage(resp),
		TokensIn:     len(packJSON) / 4,
		TokensOut:    len(resp) / 4,
		Timestamp:    nowFunc().UTC().Format("2006-01-02T15:04:05Z"),
	}

	rp.mu.Lock()
	rp.entries = append(rp.entries, entry)
	rp.mu.Unlock()

	return resp, nil
}

// Entries returns all recorded entries.
func (rp *RecordingProvider) Entries() []RecordEntry {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return append([]RecordEntry{}, rp.entries...)
}

// SaveTo writes all recorded entries to a JSONL file.
func (rp *RecordingProvider) SaveTo(path string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("recorder: create %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, entry := range rp.entries {
		if err := enc.Encode(entry); err != nil {
			return fmt.Errorf("recorder: encode: %w", err)
		}
	}
	return nil
}

// TokenDrift records the discrepancy between recorded and actual token counts.
type TokenDrift struct {
	MissionID  string  `json:"mission_id"`
	TurnNumber int     `json:"turn_number"`
	RecordedIn int     `json:"recorded_in"`
	ActualIn   int     `json:"actual_in"`
	DriftPct   float64 `json:"drift_pct"`
}

// ReplayProvider replays recorded responses without calling any LLM.
// Lookup chain: turn key -> pack_hash -> mission_id fallback.
type ReplayProvider struct {
	byTurn    map[string]json.RawMessage   // "mission_id:turn" -> response (PRIMARY)
	byHash    map[string]json.RawMessage   // pack_hash -> response
	byMission map[string][]json.RawMessage // mission_id -> responses in order (FALLBACK)

	turnCounter map[string]int // per-mission call counter during replay
	tokenDrift  []TokenDrift
	// recorded token counts for drift validation
	turnTokens map[string]int // "mission_id:turn" -> recorded tokens_in

	callCount int
	mu        sync.Mutex
}

// replayKey generates a composite lookup key for turn-based replay.
func replayKey(missionID string, turn int) string {
	return fmt.Sprintf("%s:%d", missionID, turn)
}

// NewReplayProvider loads recorded entries from a JSONL file.
func NewReplayProvider(path string) (*ReplayProvider, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("replay: open %s: %w", path, err)
	}
	defer f.Close()

	rp := &ReplayProvider{
		byTurn:      make(map[string]json.RawMessage),
		byHash:      make(map[string]json.RawMessage),
		byMission:   make(map[string][]json.RawMessage),
		turnCounter: make(map[string]int),
		turnTokens:  make(map[string]int),
	}

	dec := json.NewDecoder(f)
	for dec.More() {
		var entry RecordEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("replay: decode: %w", err)
		}
		rp.byHash[entry.PackHash] = entry.Response
		rp.byMission[entry.MissionID] = append(rp.byMission[entry.MissionID], entry.Response)
		tk := replayKey(entry.MissionID, entry.TurnNumber)
		rp.byTurn[tk] = entry.Response
		if entry.TokensIn > 0 {
			rp.turnTokens[tk] = entry.TokensIn
		}
	}

	return rp, nil
}

// NewReplayProviderFromEntries creates a replay provider from in-memory entries.
func NewReplayProviderFromEntries(entries []RecordEntry) *ReplayProvider {
	rp := &ReplayProvider{
		byTurn:      make(map[string]json.RawMessage),
		byHash:      make(map[string]json.RawMessage),
		byMission:   make(map[string][]json.RawMessage),
		turnCounter: make(map[string]int),
		turnTokens:  make(map[string]int),
	}
	for _, e := range entries {
		rp.byHash[e.PackHash] = e.Response
		rp.byMission[e.MissionID] = append(rp.byMission[e.MissionID], e.Response)
		tk := replayKey(e.MissionID, e.TurnNumber)
		rp.byTurn[tk] = e.Response
		if e.TokensIn > 0 {
			rp.turnTokens[tk] = e.TokensIn
		}
	}
	return rp
}

// Send replays the recorded response. Lookup chain: turn key -> pack_hash -> mission_id fallback.
func (rp *ReplayProvider) Send(pack *MissionPack) ([]byte, error) {
	rp.mu.Lock()
	rp.callCount++
	turn := rp.turnCounter[pack.Mission.MissionID]
	rp.turnCounter[pack.Mission.MissionID] = turn + 1
	rp.mu.Unlock()

	// PRIMARY: try turn-based key
	tk := replayKey(pack.Mission.MissionID, turn)
	if resp, ok := rp.byTurn[tk]; ok {
		rp.checkDrift(pack, tk)
		return resp, nil
	}

	// SECONDARY: try exact hash match
	packJSON, _ := json.Marshal(pack)
	packHash := hashBytes(packJSON)
	if resp, ok := rp.byHash[packHash]; ok {
		return resp, nil
	}

	// FALLBACK: match by mission_id (use turn index or first)
	if resps, ok := rp.byMission[pack.Mission.MissionID]; ok && len(resps) > 0 {
		idx := turn
		if idx >= len(resps) {
			idx = 0
		}
		return resps[idx], nil
	}

	return nil, fmt.Errorf("replay: no recorded response for turn=%s pack_hash=%s mission_id=%s", tk, packHash, pack.Mission.MissionID)
}

// checkDrift records token count discrepancy for a replayed turn.
func (rp *ReplayProvider) checkDrift(pack *MissionPack, tk string) {
	recorded, ok := rp.turnTokens[tk]
	if !ok || recorded == 0 {
		return
	}
	packJSON, _ := json.Marshal(pack)
	actual := len(packJSON) / 4
	drift := float64(actual-recorded) / float64(recorded)
	if drift < 0 {
		drift = -drift
	}
	rp.mu.Lock()
	rp.tokenDrift = append(rp.tokenDrift, TokenDrift{
		MissionID:  pack.Mission.MissionID,
		TurnNumber: rp.turnCounter[pack.Mission.MissionID] - 1,
		RecordedIn: recorded,
		ActualIn:   actual,
		DriftPct:   drift,
	})
	rp.mu.Unlock()
}

// ValidateReplay returns token drift entries that exceed the tolerance (e.g. 0.1 = 10%).
func (rp *ReplayProvider) ValidateReplay(tolerance float64) []TokenDrift {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	var violations []TokenDrift
	for _, d := range rp.tokenDrift {
		if d.DriftPct > tolerance {
			violations = append(violations, d)
		}
	}
	return violations
}

// CallCount returns the number of Send() calls made during replay.
func (rp *ReplayProvider) CallCount() int {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.callCount
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8]) // short hash for readability
}
