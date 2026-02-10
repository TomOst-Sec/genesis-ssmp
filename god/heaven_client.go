package god

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HeavenClient is an HTTP client for the Heaven SSMP daemon.
type HeavenClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHeavenClient creates a client pointing at the given Heaven URL.
func NewHeavenClient(baseURL string) *HeavenClient {
	return &HeavenClient{
		BaseURL:    baseURL,
		HTTPClient: http.DefaultClient,
	}
}

// --- IR endpoints ---

// SymbolResult matches heaven.Symbol JSON shape.
type SymbolResult struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// RefResult matches heaven.Ref JSON shape.
type RefResult struct {
	ID        int64  `json:"id"`
	SymbolID  int64  `json:"symbol_id"`
	Path      string `json:"path"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	RefKind   string `json:"ref_kind"`
}

// IRBuild triggers an index build on the repo.
func (c *HeavenClient) IRBuild(repoPath string) (int, error) {
	var resp struct {
		FilesIndexed int `json:"files_indexed"`
	}
	err := c.postJSON("/ir/build", map[string]string{"repo_path": repoPath}, &resp)
	return resp.FilesIndexed, err
}

// IRSearch performs a symbol search.
func (c *HeavenClient) IRSearch(query string, topK int) ([]SymbolResult, error) {
	var resp struct {
		Symbols []SymbolResult `json:"symbols"`
	}
	err := c.getJSON(fmt.Sprintf("/ir/search?q=%s&top_k=%d", query, topK), &resp)
	return resp.Symbols, err
}

// IRSymdef looks up a symbol definition.
func (c *HeavenClient) IRSymdef(name string) ([]SymbolResult, error) {
	var resp struct {
		Symbols []SymbolResult `json:"symbols"`
	}
	err := c.getJSON(fmt.Sprintf("/ir/symdef?name=%s", name), &resp)
	return resp.Symbols, err
}

// IRCallers returns reference sites for a symbol.
func (c *HeavenClient) IRCallers(name string, topK int) ([]RefResult, error) {
	var resp struct {
		Refs []RefResult `json:"refs"`
	}
	err := c.getJSON(fmt.Sprintf("/ir/callers?name=%s&top_k=%d", name, topK), &resp)
	return resp.Refs, err
}

// --- Lease endpoints ---

// LeaseAcquireResult matches heaven.AcquireResult.
type LeaseAcquireResult struct {
	Acquired []LeaseResult `json:"acquired"`
	Denied   []string      `json:"denied"`
}

// LeaseResult matches heaven.Lease.
type LeaseResult struct {
	LeaseID    string `json:"lease_id"`
	OwnerID    string `json:"owner_id"`
	MissionID  string `json:"mission_id"`
	ScopeType  string `json:"scope_type"`
	ScopeValue string `json:"scope_value"`
	IssuedAt   string `json:"issued_at"`
}

// LeaseAcquire acquires exclusive leases on scopes.
func (c *HeavenClient) LeaseAcquire(ownerID, missionID string, scopes []Scope) (LeaseAcquireResult, error) {
	var resp LeaseAcquireResult
	err := c.postJSON("/lease/acquire", map[string]any{
		"owner_id":   ownerID,
		"mission_id": missionID,
		"scopes":     scopes,
	}, &resp)
	return resp, err
}

// --- Validate Manifest ---

// ValidateManifestResult is the response from POST /validate-manifest.
type ValidateManifestResult struct {
	Allowed       bool     `json:"allowed"`
	Reason        string   `json:"reason,omitempty"`
	MissingLeases []string `json:"missing_leases,omitempty"`
	ClockDrift    []string `json:"clock_drift,omitempty"`
}

// ValidateManifest checks that the owner holds required leases and file clocks
// have not drifted for the given manifest.
func (c *HeavenClient) ValidateManifest(ownerID, missionID string, symbolsTouched, filesTouched []string, expectedClocks map[string]int64) (ValidateManifestResult, error) {
	var resp ValidateManifestResult
	err := c.postJSON("/validate-manifest", map[string]any{
		"owner_id":        ownerID,
		"mission_id":      missionID,
		"symbols_touched": symbolsTouched,
		"files_touched":   filesTouched,
		"expected_clocks": expectedClocks,
	}, &resp)
	return resp, err
}

// --- File Clock endpoints ---

// FileClockGet returns current file clock values for the given paths.
func (c *HeavenClient) FileClockGet(paths []string) (map[string]int64, error) {
	var resp struct {
		Clocks map[string]int64 `json:"clocks"`
	}
	err := c.postJSON("/file-clock/get", map[string]any{"paths": paths}, &resp)
	return resp.Clocks, err
}

// FileClockInc increments file clocks for the given paths and returns new values.
func (c *HeavenClient) FileClockInc(paths []string) (map[string]int64, error) {
	var resp struct {
		Clocks map[string]int64 `json:"clocks"`
	}
	err := c.postJSON("/file-clock/inc", map[string]any{"paths": paths}, &resp)
	return resp.Clocks, err
}

// --- Blob endpoints ---

// PutBlob stores a blob in Heaven and returns its content-addressed ID.
func (c *HeavenClient) PutBlob(content []byte) (string, error) {
	resp, err := c.HTTPClient.Post(c.BaseURL+"/blob", "application/octet-stream", bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("heaven POST /blob: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("heaven POST /blob: status %d: %s", resp.StatusCode, body)
	}
	var result struct {
		BlobID string `json:"blob_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("heaven POST /blob: decode: %w", err)
	}
	return result.BlobID, nil
}

// --- Event endpoints ---

// AppendEvent appends a JSON event to Heaven's event log.
func (c *HeavenClient) AppendEvent(event any) error {
	var resp struct {
		Offset int64 `json:"offset"`
	}
	return c.postJSON("/event", event, &resp)
}

// --- Status endpoint ---

// HeavenStatus matches the GET /status response.
type HeavenStatus struct {
	StateRev          int64             `json:"state_rev"`
	ActiveLeasesCount int               `json:"active_leases_count"`
	HotsetSummary     map[string]string `json:"hotset_summary"`
	FileClockSummary  map[string]string `json:"file_clock_summary"`
}

// GetStatus fetches the current Heaven daemon status.
func (c *HeavenClient) GetStatus() (*HeavenStatus, error) {
	var status HeavenStatus
	if err := c.getJSON("/status", &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// TailEvents fetches the last n events from Heaven's log.
func (c *HeavenClient) TailEvents(n int) ([]json.RawMessage, error) {
	var resp struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := c.getJSON(fmt.Sprintf("/events/tail?n=%d", n), &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// --- HTTP helpers ---

func (c *HeavenClient) getJSON(path string, out any) error {
	resp, err := c.HTTPClient.Get(c.BaseURL + path)
	if err != nil {
		return fmt.Errorf("heaven GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heaven GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *HeavenClient) postJSON(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Post(c.BaseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("heaven POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heaven POST %s: status %d: %s", path, resp.StatusCode, respBody)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
