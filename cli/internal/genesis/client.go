package genesis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin wrapper around the Genesis Heaven/God HTTP APIs.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new Genesis API client pointed at the given address.
func NewClient(addr string) *Client {
	return &Client{
		BaseURL: "http://" + addr,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Status calls GET /status and returns the current Heaven state.
func (c *Client) Status() (*HeavenStatus, error) {
	var result HeavenStatus
	if err := c.getJSON("/status", &result); err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	return &result, nil
}

// TailEvents calls GET /events/tail?n=N and returns recent events.
func (c *Client) TailEvents(n int) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := c.getJSON(fmt.Sprintf("/events/tail?n=%d", n), &result); err != nil {
		return nil, fmt.Errorf("tail events: %w", err)
	}
	return result, nil
}

// IRBuild calls POST /ir/build to index a repository.
func (c *Client) IRBuild(repoPath string) (int, error) {
	body := map[string]string{"repo_path": repoPath}
	var result struct {
		Symbols int `json:"symbols"`
	}
	if err := c.postJSON("/ir/build", body, &result); err != nil {
		return 0, fmt.Errorf("ir build: %w", err)
	}
	return result.Symbols, nil
}

// IRSearch calls POST /ir/search for semantic symbol search.
func (c *Client) IRSearch(query string, topK int) ([]SymbolResult, error) {
	body := map[string]any{"query": query, "top_k": topK}
	var result []SymbolResult
	if err := c.postJSON("/ir/search", body, &result); err != nil {
		return nil, fmt.Errorf("ir search: %w", err)
	}
	return result, nil
}

// IRSymdef calls POST /ir/symdef to find symbol definitions.
func (c *Client) IRSymdef(name string) ([]SymbolResult, error) {
	body := map[string]string{"name": name}
	var result []SymbolResult
	if err := c.postJSON("/ir/symdef", body, &result); err != nil {
		return nil, fmt.Errorf("ir symdef: %w", err)
	}
	return result, nil
}

// IRCallers calls POST /ir/callers to find callers of a symbol.
func (c *Client) IRCallers(name string, topK int) ([]RefResult, error) {
	body := map[string]any{"name": name, "top_k": topK}
	var result []RefResult
	if err := c.postJSON("/ir/callers", body, &result); err != nil {
		return nil, fmt.Errorf("ir callers: %w", err)
	}
	return result, nil
}

// LeaseAcquire calls POST /lease/acquire to obtain a file lease.
func (c *Client) LeaseAcquire(ownerID, missionID string, scopes []Scope) (*LeaseAcquireResult, error) {
	body := map[string]any{
		"owner_id":   ownerID,
		"mission_id": missionID,
		"scopes":     scopes,
	}
	var result LeaseAcquireResult
	if err := c.postJSON("/lease/acquire", body, &result); err != nil {
		return nil, fmt.Errorf("lease acquire: %w", err)
	}
	return &result, nil
}

// ValidateManifest calls POST /validate-manifest.
func (c *Client) ValidateManifest(manifest any) (*ValidateManifestResult, error) {
	var result ValidateManifestResult
	if err := c.postJSON("/validate-manifest", manifest, &result); err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}
	return &result, nil
}

// PutBlob calls POST /blob to store content and returns the blob hash.
func (c *Client) PutBlob(content []byte) (string, error) {
	resp, err := c.HTTPClient.Post(c.BaseURL+"/blob", "application/octet-stream", bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("put blob: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("put blob: status %d", resp.StatusCode)
	}
	var result struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("put blob decode: %w", err)
	}
	return result.Hash, nil
}

// AppendEvent calls POST /events to append an event to the log.
func (c *Client) AppendEvent(event any) error {
	return c.postJSON("/events", event, nil)
}

// FileClockGet calls POST /fileclock/get.
func (c *Client) FileClockGet(paths []string) (map[string]int64, error) {
	body := map[string]any{"paths": paths}
	var result map[string]int64
	if err := c.postJSON("/fileclock/get", body, &result); err != nil {
		return nil, fmt.Errorf("fileclock get: %w", err)
	}
	return result, nil
}

// FileClockInc calls POST /fileclock/inc.
func (c *Client) FileClockInc(paths []string) (map[string]int64, error) {
	body := map[string]any{"paths": paths}
	var result map[string]int64
	if err := c.postJSON("/fileclock/inc", body, &result); err != nil {
		return nil, fmt.Errorf("fileclock inc: %w", err)
	}
	return result, nil
}

// getJSON performs a GET request and decodes the JSON response.
func (c *Client) getJSON(path string, target any) error {
	resp, err := c.HTTPClient.Get(c.BaseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// postJSON performs a POST request with JSON body and decodes the response.
func (c *Client) postJSON(path string, body any, target any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	resp, err := c.HTTPClient.Post(c.BaseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}
