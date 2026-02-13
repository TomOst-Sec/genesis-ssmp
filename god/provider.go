package god

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EditOp represents a single Edit IR operation returned by an Angel.
type EditOp struct {
	Op         string              `json:"op"`
	Path       string              `json:"path"`
	AnchorHash string              `json:"anchor_hash"`
	Lines      []int               `json:"lines,omitempty"`
	Content    string              `json:"content,omitempty"`
	Symbol     string              `json:"symbol,omitempty"`
	Template   string              `json:"template,omitempty"`
	Instances  []map[string]string `json:"instances,omitempty"`
}

// EditIR is the intermediate representation for code modifications.
type EditIR struct {
	Ops []EditOp `json:"ops"`
}

// Manifest lists symbols and files touched by an Angel response.
type Manifest struct {
	SymbolsTouched []string `json:"symbols_touched"`
	FilesTouched   []string `json:"files_touched"`
}

// AngelResponse is the structured output from an Angel after executing a mission.
type AngelResponse struct {
	MissionID  string    `json:"mission_id"`
	OutputType string    `json:"output_type"` // "edit_ir", "diff_fallback", or "macro_ops"
	EditIR     *EditIR   `json:"edit_ir,omitempty"`
	MacroOps   *MacroOps `json:"macro_ops,omitempty"`
	Diff       string    `json:"diff,omitempty"`
	Manifest   Manifest  `json:"manifest"`
}

// ProviderUsage records metering data for a provider call.
type ProviderUsage struct {
	MissionID     string `json:"mission_id"`
	RequestBytes  int    `json:"request_bytes"`
	ResponseBytes int    `json:"response_bytes"`
	Retries       int    `json:"retries"`
	DurationMS    int64  `json:"duration_ms"`
	Success       bool   `json:"success"`
}

// CLITokenUsage captures real token usage data from the Claude CLI JSON output.
type CLITokenUsage struct {
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	NumTurns                 int     `json:"num_turns"`
	DurationMS               int64   `json:"duration_ms"`
}

// Provider is the interface for sending mission packs to a cloud LLM and
// receiving structured Angel responses.
type Provider interface {
	// Send transmits a mission pack and returns the raw response bytes.
	Send(pack *MissionPack) ([]byte, error)
}

// CLIUsageProvider is an optional interface that providers can implement
// to expose real token usage data from the underlying CLI call.
type CLIUsageProvider interface {
	CLIUsage() *CLITokenUsage
}

// ProviderAdapter wraps a Provider with response validation, retry logic,
// and usage metering.
type ProviderAdapter struct {
	provider   Provider
	maxRetries int
}

// NewProviderAdapter creates a ProviderAdapter with the given Provider backend.
func NewProviderAdapter(provider Provider) *ProviderAdapter {
	return &ProviderAdapter{
		provider:   provider,
		maxRetries: 1,
	}
}

// Execute sends a mission pack to the provider, validates the response,
// retries once on schema violation, and returns the parsed AngelResponse
// along with usage metrics.
func (pa *ProviderAdapter) Execute(pack *MissionPack) (*AngelResponse, *ProviderUsage, error) {
	packJSON, err := json.Marshal(pack)
	if err != nil {
		return nil, nil, fmt.Errorf("provider: marshal pack: %w", err)
	}

	// Soft enforcement: warn if pack is suspiciously large when PromptRef is set
	if pack.PromptRef != nil && pack.PromptRef.TotalTokens > 0 {
		threshold := pack.PromptRef.TotalTokens * 4
		if len(packJSON) > threshold {
			// Log warning — full prompt may have leaked into the pack
			fmt.Printf("WARNING: pack bytes %d > prompt total_tokens*4 (%d) — possible full prompt leak\n",
				len(packJSON), threshold)
		}
	}

	usage := &ProviderUsage{
		MissionID:    pack.Mission.MissionID,
		RequestBytes: len(packJSON),
	}

	start := nowFunc()
	var resp *AngelResponse

	for attempt := 0; attempt <= pa.maxRetries; attempt++ {
		raw, err := pa.provider.Send(pack)
		if err != nil {
			usage.DurationMS = nowFunc().Sub(start).Milliseconds()
			return nil, usage, fmt.Errorf("provider: send: %w", err)
		}

		usage.ResponseBytes = len(raw)

		resp, err = parseAngelResponse(raw)
		if err == nil {
			err = validateAngelResponse(resp, pack.Mission.MissionID)
		}

		if err == nil {
			// Auto-expand macro_ops to edit_ir transparently
			if resp.OutputType == "macro_ops" && resp.MacroOps != nil {
				expanded, expErr := ExpandMacroOps(resp.MacroOps)
				if expErr != nil {
					usage.DurationMS = nowFunc().Sub(start).Milliseconds()
					return nil, usage, fmt.Errorf("provider: macro expansion: %w", expErr)
				}
				resp.EditIR = expanded
				resp.OutputType = "edit_ir"
			}
			break
		}

		// On schema violation, retry with repair prompt (once)
		if attempt < pa.maxRetries {
			usage.Retries++
			pack = buildRepairPack(pack, raw, err)
			continue
		}

		usage.DurationMS = nowFunc().Sub(start).Milliseconds()
		return nil, usage, fmt.Errorf("provider: validation failed after %d retries: %w", usage.Retries, err)
	}

	usage.DurationMS = nowFunc().Sub(start).Milliseconds()
	usage.Success = true
	return resp, usage, nil
}

// parseAngelResponse unmarshals raw bytes into an AngelResponse.
func parseAngelResponse(raw []byte) (*AngelResponse, error) {
	var resp AngelResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &resp, nil
}

// validateAngelResponse checks that an AngelResponse conforms to the schema.
func validateAngelResponse(resp *AngelResponse, expectedMissionID string) error {
	if resp.MissionID == "" {
		return fmt.Errorf("response missing mission_id")
	}
	if resp.MissionID != expectedMissionID {
		return fmt.Errorf("response mission_id %q does not match expected %q", resp.MissionID, expectedMissionID)
	}

	switch resp.OutputType {
	case "edit_ir":
		if resp.EditIR == nil {
			return fmt.Errorf("output_type is edit_ir but edit_ir field is nil")
		}
		if resp.EditIR.Ops == nil {
			return fmt.Errorf("edit_ir.ops must not be nil")
		}
		for i, op := range resp.EditIR.Ops {
			if err := validateEditOp(op, i); err != nil {
				return err
			}
		}
	case "macro_ops":
		if resp.MacroOps == nil {
			return fmt.Errorf("output_type is macro_ops but macro_ops field is nil")
		}
		if resp.MacroOps.Ops == nil {
			return fmt.Errorf("macro_ops.ops must not be nil")
		}
		for i, op := range resp.MacroOps.Ops {
			if err := ValidateMacroOp(op, i); err != nil {
				return err
			}
		}
	case "diff_fallback":
		if resp.Diff == "" {
			return fmt.Errorf("output_type is diff_fallback but diff field is empty")
		}
	default:
		return fmt.Errorf("invalid output_type %q (expected edit_ir, macro_ops, or diff_fallback)", resp.OutputType)
	}

	if resp.Manifest.SymbolsTouched == nil {
		return fmt.Errorf("manifest.symbols_touched must not be nil")
	}
	if resp.Manifest.FilesTouched == nil {
		return fmt.Errorf("manifest.files_touched must not be nil")
	}

	return nil
}

// validateEditOp checks a single Edit IR operation.
func validateEditOp(op EditOp, index int) error {
	switch op.Op {
	case "replace_span", "insert_after_symbol", "add_file", "delete_span",
		"insert_before_symbol", "delete_file", "replace_line", "insert_lines", "template":
		// valid
	default:
		return fmt.Errorf("ops[%d]: invalid op %q", index, op.Op)
	}
	if op.Path == "" {
		return fmt.Errorf("ops[%d]: path is required", index)
	}
	// anchor_hash is optional for add_file and delete_file
	if op.AnchorHash == "" && op.Op != "add_file" && op.Op != "delete_file" {
		return fmt.Errorf("ops[%d]: anchor_hash is required", index)
	}
	// template op requires Template and Instances
	if op.Op == "template" {
		if op.Template == "" {
			return fmt.Errorf("ops[%d]: template field is required for template op", index)
		}
		if len(op.Instances) == 0 {
			return fmt.Errorf("ops[%d]: instances field is required for template op", index)
		}
	}
	return nil
}

// maxRepairResponseBytes caps the echoed invalid response in repair packs.
// Echoing the full response can double pack size and waste 50%+ of budget.
const maxRepairResponseBytes = 512

// buildRepairPack creates a new mission pack with a repair prompt appended,
// including a truncated invalid response and the error message.
func buildRepairPack(original *MissionPack, invalidResponse []byte, validationErr error) *MissionPack {
	// Truncate the invalid response to prevent token bomb
	respSnippet := invalidResponse
	if len(respSnippet) > maxRepairResponseBytes {
		respSnippet = append(respSnippet[:maxRepairResponseBytes], []byte("\n... [truncated]")...)
	}

	repairHeader := original.Header + "\n\n" +
		"--- REPAIR REQUEST ---\n" +
		"Your previous response failed validation.\n" +
		"Error: " + validationErr.Error() + "\n" +
		"Response snippet:\n" + string(respSnippet) + "\n" +
		"Please return a corrected response matching the required JSON schema.\n" +
		"Required: {\"mission_id\": \"...\", \"output_type\": \"edit_ir\"|\"macro_ops\"|\"diff_fallback\", " +
		"\"edit_ir\": {\"ops\": [...]}, \"manifest\": {\"symbols_touched\": [...], \"files_touched\": [...]}}"

	return &MissionPack{
		Header:       repairHeader,
		Mission:      original.Mission,
		InlineShards: original.InlineShards,
		PFEndpoint:   original.PFEndpoint,
		BudgetMeta:   original.BudgetMeta,
	}
}

// --- HTTP Provider implementation ---

// HTTPProvider sends mission packs to a cloud LLM via HTTP POST.
type HTTPProvider struct {
	Endpoint   string
	APIKey     string
	HTTPClient *http.Client
}

// NewHTTPProvider creates an HTTPProvider targeting the given endpoint.
func NewHTTPProvider(endpoint, apiKey string) *HTTPProvider {
	return &HTTPProvider{
		Endpoint:   endpoint,
		APIKey:     apiKey,
		HTTPClient: http.DefaultClient,
	}
}

// Send posts the mission pack JSON to the provider endpoint and returns
// the raw response body.
func (p *HTTPProvider) Send(pack *MissionPack) ([]byte, error) {
	data, err := json.Marshal(pack)
	if err != nil {
		return nil, fmt.Errorf("http provider: marshal: %w", err)
	}

	req, err := http.NewRequest("POST", p.Endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("http provider: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http provider: do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http provider: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http provider: status %d: %s", resp.StatusCode, body)
	}

	return body, nil
}
