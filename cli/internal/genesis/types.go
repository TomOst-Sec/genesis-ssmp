package genesis

import "encoding/json"

// HeavenStatus represents the response from GET /status.
type HeavenStatus struct {
	StateRev int64             `json:"state_rev"`
	Leases   []LeaseInfo       `json:"leases"`
	Clocks   map[string]int64  `json:"clocks"`
}

// LeaseInfo describes an active lease returned in HeavenStatus.
type LeaseInfo struct {
	ID        string   `json:"id"`
	OwnerID   string   `json:"owner_id"`
	MissionID string   `json:"mission_id"`
	Scopes    []Scope  `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
}

// Scope defines a file-level lock scope for a lease.
type Scope struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // "read" or "write"
}

// SymbolResult is returned by IR search and symdef endpoints.
type SymbolResult struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Score    float64 `json:"score,omitempty"`
}

// RefResult is returned by the IR callers endpoint.
type RefResult struct {
	Caller   string `json:"caller"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Score    float64 `json:"score,omitempty"`
}

// LeaseAcquireResult is the response from POST /lease/acquire.
type LeaseAcquireResult struct {
	LeaseID   string `json:"lease_id"`
	ExpiresAt string `json:"expires_at"`
	Granted   bool   `json:"granted"`
	Reason    string `json:"reason,omitempty"`
}

// ValidateManifestResult is the response from POST /validate-manifest.
type ValidateManifestResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// Event represents an opaque event from the Heaven event log.
type Event = json.RawMessage
