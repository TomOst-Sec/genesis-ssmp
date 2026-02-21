package god

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/genesis-ssmp/genesis/heaven"
)

const nirvanaAngelPrompt = `You are a Genesis Angel — an AI code editor that receives mission packs and produces Edit IR (structured code modifications).

You MUST respond with ONLY a single JSON object matching this exact schema:

{
  "mission_id": "<same mission_id from the input mission>",
  "output_type": "edit_ir",
  "edit_ir": {
    "ops": [
      {
        "op": "replace_span|insert_after_symbol|add_file|delete_span",
        "path": "relative/file/path",
        "anchor_hash": "placeholder",
        "lines": [start_line, end_line],
        "content": "new content"
      }
    ]
  },
  "manifest": {
    "symbols_touched": ["symbol1", "symbol2"],
    "files_touched": ["file1.py", "file2.py"]
  }
}

OPERATION TYPES:
- "add_file": Create a new file. Set "content" to the full file content. "anchor_hash" = "placeholder".
- "replace_span": Replace lines [start_line, end_line] (1-indexed, inclusive) with "content". "anchor_hash" = "placeholder".
- "delete_span": Delete lines [start_line, end_line]. "anchor_hash" = "placeholder".
- "insert_after_symbol": Insert "content" after the line containing "symbol". "anchor_hash" = "placeholder".

RULES:
- Output ONLY the JSON object. No markdown, no explanation, no code fences.
- Set all anchor_hash values to "placeholder" — they will be computed automatically.
- The mission_id in your response MUST exactly match the mission_id from the input.
- manifest.files_touched MUST list every file path you modify or create.
- manifest.symbols_touched MUST list every function/class you modify or create.
- For existing files, prefer "replace_span" with precise line ranges.
- For new files, use "add_file" with the complete file content.
- Lines are 1-indexed and inclusive on both ends.`

const nirvanaAngelPromptPatchV1 = `You are a Genesis Angel. Output ONLY GENESIS_PATCH_V1 text. Do NOT use tools. Do NOT output JSON, markdown, code fences, or prose.

FORMAT:
GENESIS_PATCH_V1
MISSION: <mission_id from input>
SYMBOLS: sym1, sym2
FILES: file1.py, file2.py
### REPLACE path 17 30
<delta-encoded content>
### END
### ADD path/new_file.py
<full file content>
### END
### DELETE path 50 55
### END

OPS: REPLACE path start end (1-indexed inclusive), ADD path, DELETE path start end, INSERT_AFTER path symbol.

DELTA ENCODING (REPLACE only):
  @K     — keep 1 original line
  @K{N}  — keep N lines (e.g. @K8 keeps 8 lines)
  @S     — skip/delete 1 original line
  @S{N}  — skip N lines
  @@...  — escape literal @ (@@property → @property)
  <text> — literal new line (does NOT advance position)
The decoder tracks position through original [start,end]. @K/@S advance; literals insert. Remaining original lines are implicitly deleted.

EXAMPLE — insert new branch into function (lines 50-58, 9 lines):
### REPLACE src/engine.py 50 58
@K8
    elif op == 'star':
        return gen_star(node, nfa)
@K
### END
Result: @K8 keeps lines 50-57, two literals insert new code, @K keeps line 58. 4 output lines instead of 11.

EXAMPLE — replace one line in a span (lines 100-110, change line 105):
### REPLACE src/parser.py 100 110
@K5
    new_code_here()
@S
@K5
### END
@K5 keeps 100-104, literal replaces 105, @S skips old 105, @K5 keeps 106-110.

EXAMPLE — new file:
### ADD tests/test_star.py
import pytest
def test_star():
    assert match("a*", "aaa")
### END

RULES:
1. Output ONLY GENESIS_PATCH_V1 text — nothing else.
2. MISSION must match mission_id from input.
3. SYMBOLS/FILES must list all touched symbols/files.
4. REPLACE: use @K for unchanged lines, NEVER repeat original text.
5. ADD: full literal content (no @K/@S).
6. DELETE: no content between header and ### END.
7. Lines are 1-indexed, inclusive both ends.
8. Escape @ lines with @@ prefix.
9. Prefer REPLACE with precise ranges. Use ADD only for new files.`

// nirvanaCLIProvider implements Provider by calling claude CLI subprocess.
type nirvanaCLIProvider struct {
	model          string
	repoRoot       string
	outputFormat   string // "json" (default) or "patch_v1"
	tokensIn       int
	tokensOut      int
	cacheCreateIn  int
	cacheReadIn    int
	callCount      int
	totalCostUSD   float64
	lastOutput     string
	rawCLIOutput   []byte // full JSON from claude CLI
}

func (p *nirvanaCLIProvider) Send(pack *MissionPack) ([]byte, error) {
	p.callCount++

	// Build prompt from pack
	var sb strings.Builder
	sb.WriteString(pack.Header)
	sb.WriteString("\n\n")

	missionJSON, _ := json.MarshalIndent(pack.Mission, "", "  ")
	sb.WriteString("MISSION:\n")
	sb.WriteString(string(missionJSON))
	sb.WriteString("\n\n")

	if len(pack.InlineShards) > 0 {
		sb.WriteString("CODE CONTEXT (from IR index):\n")
		for _, shard := range pack.InlineShards {
			fmt.Fprintf(&sb, "--- %s [%s] ---\n", shard.Kind, shard.BlobID)
			sb.WriteString(string(shard.Content))
			sb.WriteString("\n\n")
		}
	}

	// Also include source files that the mission references
	for _, scope := range pack.Mission.Scopes {
		if scope.ScopeType == "file" {
			absPath := p.repoRoot + "/" + scope.ScopeValue
			data, err := os.ReadFile(absPath)
			if err == nil {
				fmt.Fprintf(&sb, "SOURCE FILE: %s\n", scope.ScopeValue)
				sb.WriteString(string(data))
				sb.WriteString("\n\n")
			}
		}
	}

	// Select prompt and instruction based on output format
	systemPrompt := nirvanaAngelPrompt
	if p.outputFormat == "patch_v1" {
		systemPrompt = nirvanaAngelPromptPatchV1
		sb.WriteString("Execute this mission. Return ONLY the GENESIS_PATCH_V1 text.")
	} else {
		sb.WriteString("Execute this mission. Return ONLY the AngelResponse JSON.")
	}
	prompt := sb.String()

	// Track input tokens (estimate)
	p.tokensIn += len(prompt)/4 + 10

	// Call claude CLI
	args := []string{
		"-p",
		"--model", p.model,
		"--output-format", "json",
		"--no-session-persistence",
		"--system-prompt", systemPrompt,
	}
	if p.outputFormat == "patch_v1" {
		// Disable all tools so the model outputs GENESIS_PATCH_V1 text directly.
		// Without this, the model wastes turns trying Edit/WebFetch instead of text output.
		args = append(args, "--tools", "", "--max-turns", "1")
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = p.repoRoot
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude CLI (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI: %w", err)
	}

	// Save raw CLI output for cost analysis
	p.rawCLIOutput = out

	// Parse claude JSON output
	var result struct {
		Type         string  `json:"type"`
		Result       string  `json:"result"`
		IsError      bool    `json:"is_error"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		NumTurns     int     `json:"num_turns"`
		Usage        struct {
			InputTokens             int `json:"input_tokens"`
			OutputTokens            int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens    int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse claude output: %w", err)
	}
	if result.IsError {
		return nil, fmt.Errorf("claude error: %s", result.Result)
	}

	// Update actual token counts from provider
	if result.Usage.InputTokens > 0 {
		p.tokensIn = result.Usage.InputTokens
	}
	p.tokensOut += result.Usage.OutputTokens
	p.cacheCreateIn += result.Usage.CacheCreationInputTokens
	p.cacheReadIn += result.Usage.CacheReadInputTokens
	p.totalCostUSD += result.TotalCostUSD

	text := result.Result
	p.lastOutput = text

	if p.outputFormat == "patch_v1" {
		// Parse GENESIS_PATCH_V1 format
		// Strip code fences if present
		cleaned := strings.TrimSpace(text)
		if after, found := strings.CutPrefix(cleaned, "```"); found {
			cleaned = after
			// Remove optional language tag on same line
			if idx := strings.Index(cleaned, "\n"); idx >= 0 {
				cleaned = cleaned[idx+1:]
			}
		}
		if after, found := strings.CutSuffix(strings.TrimSpace(cleaned), "```"); found {
			cleaned = strings.TrimSpace(after)
		}

		// Strip any preamble text before the GENESIS_PATCH_V1 header.
		// Use LastIndex to handle cases where the model outputs multiple
		// GENESIS_PATCH_V1 blocks (self-correction) — take the last one.
		if idx := strings.LastIndex(cleaned, "GENESIS_PATCH_V1\n"); idx > 0 {
			cleaned = cleaned[idx:]
		}

		ps, err := ParsePatchV1([]byte(cleaned))
		if err != nil {
			return nil, fmt.Errorf("parse patch_v1: %w", err)
		}
		// Decode delta-encoded REPLACE content using original files
		if err := DecodePatchV1Set(ps, p.repoRoot); err != nil {
			return nil, fmt.Errorf("decode patch_v1: %w", err)
		}
		resp, err := PatchV1ToAngelResponse(ps)
		if err != nil {
			return nil, fmt.Errorf("convert patch_v1: %w", err)
		}
		respJSON, _ := json.Marshal(resp)
		// Fix anchor hashes
		fixed, err := fixAngelAnchorHashes(respJSON, p.repoRoot)
		if err != nil {
			return respJSON, nil
		}
		return fixed, nil
	}

	// JSON mode: strip code fences and extract JSON
	text = strings.TrimSpace(text)
	if after, found := strings.CutPrefix(text, "```json"); found {
		text = after
	} else if after, found := strings.CutPrefix(text, "```"); found {
		text = after
	}
	if after, found := strings.CutSuffix(text, "```"); found {
		text = after
	}
	text = strings.TrimSpace(text)

	// Extract JSON
	jsonText := extractJSONFromText(text)

	// Fix anchor hashes before returning
	fixed, err := fixAngelAnchorHashes([]byte(jsonText), p.repoRoot)
	if err != nil {
		return []byte(jsonText), nil // return unfixed if fixing fails
	}
	return fixed, nil
}

func extractJSONFromText(text string) string {
	start := strings.Index(text, "{")
	if start == -1 {
		return text
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		case '"':
			i++
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return text[start:]
}

// fixAngelAnchorHashes computes real anchor hashes by simulating sequential
// application. It also deduplicates overlapping ops and sorts ops within each
// file bottom-to-top so line shifts don't corrupt earlier ops.
func fixAngelAnchorHashes(raw []byte, repoRoot string) ([]byte, error) {
	var resp AngelResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.EditIR == nil {
		return raw, nil
	}

	// Step 1: Remove overlapping ops. If op A is strictly contained within
	// op B (same file, A's range inside B's range), drop A.
	dedupOps := make([]EditOp, 0, len(resp.EditIR.Ops))
	for i, op := range resp.EditIR.Ops {
		if op.Op == "add_file" || op.Op == "insert_after_symbol" || len(op.Lines) != 2 {
			dedupOps = append(dedupOps, op)
			continue
		}
		subsumed := false
		for j, other := range resp.EditIR.Ops {
			if i == j || other.Op == "add_file" || other.Op == "insert_after_symbol" || len(other.Lines) != 2 {
				continue
			}
			if op.Path == other.Path &&
				other.Lines[0] <= op.Lines[0] && other.Lines[1] >= op.Lines[1] &&
				(other.Lines[0] < op.Lines[0] || other.Lines[1] > op.Lines[1]) {
				subsumed = true
				break
			}
		}
		if !subsumed {
			dedupOps = append(dedupOps, op)
		}
	}

	// Step 2: Sort ops within each file by start line DESCENDING (bottom-up).
	// This ensures that line-number shifts from earlier ops don't affect later ones.
	// Ops on different files or add_file ops keep relative order.
	sort.SliceStable(dedupOps, func(i, j int) bool {
		oi, oj := dedupOps[i], dedupOps[j]
		// add_file ops go to the end
		if oi.Op == "add_file" && oj.Op != "add_file" {
			return false
		}
		if oi.Op != "add_file" && oj.Op == "add_file" {
			return true
		}
		if oi.Path != oj.Path {
			return oi.Path < oj.Path
		}
		// Same file: higher start line comes first
		startI, startJ := 0, 0
		if len(oi.Lines) == 2 {
			startI = oi.Lines[0]
		}
		if len(oj.Lines) == 2 {
			startJ = oj.Lines[0]
		}
		return startI > startJ
	})

	resp.EditIR.Ops = dedupOps

	// Step 3: Simulate sequential application. For each op, compute anchor
	// from the current in-memory file state, then apply the edit to the cache.
	fileCache := make(map[string][]string) // path -> current lines

	for i, op := range resp.EditIR.Ops {
		switch op.Op {
		case "add_file":
			resp.EditIR.Ops[i].AnchorHash = ComputeAnchorHash(nil, 0, 0)
			if op.Content != "" {
				fileCache[op.Path] = splitContent(op.Content)
			}

		case "replace_span":
			if len(op.Lines) != 2 {
				continue
			}
			lines := getCachedLines(fileCache, repoRoot, op.Path)
			if lines == nil || op.Lines[1] > len(lines) {
				continue
			}
			// Compute anchor from current state
			resp.EditIR.Ops[i].AnchorHash = ComputeAnchorHash(lines, op.Lines[0], op.Lines[1])
			// Simulate apply: replace the span
			newContent := splitContent(op.Content)
			result := make([]string, 0, len(lines))
			result = append(result, lines[:op.Lines[0]-1]...)
			result = append(result, newContent...)
			result = append(result, lines[op.Lines[1]:]...)
			fileCache[op.Path] = result

		case "delete_span":
			if len(op.Lines) != 2 {
				continue
			}
			lines := getCachedLines(fileCache, repoRoot, op.Path)
			if lines == nil || op.Lines[1] > len(lines) {
				continue
			}
			resp.EditIR.Ops[i].AnchorHash = ComputeAnchorHash(lines, op.Lines[0], op.Lines[1])
			result := make([]string, 0, len(lines))
			result = append(result, lines[:op.Lines[0]-1]...)
			result = append(result, lines[op.Lines[1]:]...)
			fileCache[op.Path] = result

		case "insert_after_symbol":
			lines := getCachedLines(fileCache, repoRoot, op.Path)
			if lines == nil {
				continue
			}
			for j, line := range lines {
				if strings.Contains(line, op.Symbol) {
					resp.EditIR.Ops[i].AnchorHash = ComputeAnchorHash(lines, j+1, j+1)
					newContent := splitContent(op.Content)
					result := make([]string, 0, len(lines)+len(newContent))
					result = append(result, lines[:j+1]...)
					result = append(result, newContent...)
					result = append(result, lines[j+1:]...)
					fileCache[op.Path] = result
					break
				}
			}
		}
	}

	return json.Marshal(resp)
}

// getCachedLines returns the in-memory file lines, loading from disk on first access.
func getCachedLines(cache map[string][]string, repoRoot, path string) []string {
	if lines, ok := cache[path]; ok {
		return lines
	}
	absPath := repoRoot + "/" + path
	lines, err := readFileLines(absPath)
	if err != nil {
		return nil
	}
	cache[path] = lines
	return lines
}

// TestNirvanaGenesis runs the full Genesis pipeline on tinygrep and captures metrics.
// Run with: go test -run TestNirvanaGenesis -v -timeout 600s
func TestNirvanaGenesis(t *testing.T) {
	repoPath := "/tmp/tinygrep-plain"
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Skip("tinygrep-plain not cloned — clone first")
	}

	// Check claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}

	taskDesc := `Improve this project`

	// Start Heaven
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewHeavenClient(ts.URL)

	// Create provider
	provider := &nirvanaCLIProvider{
		model:    "claude-opus-4-6",
		repoRoot: repoPath,
	}

	// Configure solo executor
	config := SoloConfig{
		TokenBudget:  32000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: false, // Allow non-Edit-IR for initial run
	}

	executor := NewSoloExecutor(client, provider, config)

	// Execute
	t.Log("=== GENESIS EXECUTION START ===")
	start := time.Now()
	result, err := executor.Execute(taskDesc, repoPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Execute error: %v", err)
	}

	// Write metrics to file for the comparison report
	metrics := map[string]any{
		"tokens_in":            provider.tokensIn,
		"tokens_out":           provider.tokensOut,
		"cache_creation_in":    provider.cacheCreateIn,
		"cache_read_in":        provider.cacheReadIn,
		"total_cost_usd":       provider.totalCostUSD,
		"provider_calls":       provider.callCount,
		"elapsed_sec":          elapsed.Seconds(),
		"success":              result != nil && result.Success,
		"error":                "",
	}
	if result != nil {
		metrics["files_modified"] = result.FilesModified
		metrics["files_created"] = result.FilesCreated
		metrics["turns"] = result.Turns
		metrics["pf_calls"] = result.PFCalls
		metrics["pack_tokens_in"] = result.TokensIn
		metrics["pack_tokens_out"] = result.TokensOut
		if result.Error != "" {
			metrics["error"] = result.Error
		}
	}
	if err != nil {
		metrics["error"] = err.Error()
	}

	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	os.WriteFile("/tmp/nirvana-report/genesis_metrics.json", metricsJSON, 0o644)

	// Save the raw Angel output for inspection
	os.WriteFile("/tmp/nirvana-report/genesis_angel_output.txt", []byte(provider.lastOutput), 0o644)
	// Save the raw CLI JSON for cost verification
	os.WriteFile("/tmp/nirvana-report/genesis_cli_raw.json", provider.rawCLIOutput, 0o644)

	// Print results
	t.Log("=== GENESIS METRICS ===")
	t.Logf("  tokens_in (provider):  %d", provider.tokensIn)
	t.Logf("  tokens_out (provider): %d", provider.tokensOut)
	t.Logf("  cache_create_in:       %d", provider.cacheCreateIn)
	t.Logf("  cache_read_in:         %d", provider.cacheReadIn)
	t.Logf("  total_cost_usd:        $%.6f", provider.totalCostUSD)
	t.Logf("  provider_calls:        %d", provider.callCount)
	t.Logf("  elapsed:               %s", elapsed)
	if result != nil {
		t.Logf("  success:               %v", result.Success)
		t.Logf("  files_modified:        %v", result.FilesModified)
		t.Logf("  files_created:         %v", result.FilesCreated)
		t.Logf("  pack tokens_in:        %d", result.TokensIn)
		t.Logf("  pack tokens_out:       %d", result.TokensOut)
		t.Logf("  turns:                 %d", result.Turns)
		if result.Error != "" {
			t.Logf("  error:                 %s", result.Error)
		}
	}

	// Run tests in the repo
	t.Log("=== RUNNING TESTS ===")
	testCmd := exec.Command("bash", "-c",
		fmt.Sprintf("cd %s && source .venv/bin/activate && pytest -v 2>&1; deactivate", repoPath))
	testOut, testErr := testCmd.CombinedOutput()
	t.Logf("pytest output:\n%s", string(testOut))
	if testErr != nil {
		t.Logf("pytest exit error: %v", testErr)
	}

	// Save test output
	os.WriteFile("/tmp/nirvana-report/genesis_test_output.txt", testOut, 0o644)

	// Capture git diff
	diffCmd := exec.Command("git", "-C", repoPath, "diff")
	diffOut, _ := diffCmd.Output()
	os.WriteFile("/tmp/nirvana-report/genesis_diff.txt", diffOut, 0o644)

	diffStatCmd := exec.Command("git", "-C", repoPath, "diff", "--stat")
	diffStatOut, _ := diffStatCmd.Output()
	t.Logf("git diff --stat:\n%s", string(diffStatOut))

	// New files
	statusCmd := exec.Command("git", "-C", repoPath, "status", "--short")
	statusOut, _ := statusCmd.Output()
	t.Logf("git status:\n%s", string(statusOut))
	os.WriteFile("/tmp/nirvana-report/genesis_git_status.txt", statusOut, 0o644)
}

// TestNirvanaPatchV1 runs the Genesis pipeline with GENESIS_PATCH_V1 output format.
// Run with: go test -run TestNirvanaPatchV1 -v -timeout 600s
func TestNirvanaPatchV1(t *testing.T) {
	repoPath := "/tmp/tinygrep-patch"
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Skip("tinygrep-patch not cloned — clone to /tmp/tinygrep-patch first")
	}

	// Check claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}

	taskDesc := `Add support for the * (zero-or-more) quantifier to tinygrep's regex engine. This requires:

1) Update the regex parser to recognize * as a quantifier token (in src/tinygrep/cli.py)
2) Implement the * quantifier in the matching engine (greedy by default) — follow the same pattern used by + and ? in the gen() function
3) Add a test file tests/test_star.py with unit tests for * including edge cases:
   - a* matching empty string
   - a* matching "aaa"
   - .* matching any string
   - [abc]* with character classes
   - (foo)* group repetition
4) Update the README.md to document the new * quantifier in the regex features section

Run the tests to verify everything passes.`

	// Start Heaven
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewHeavenClient(ts.URL)

	// Create provider with patch_v1 output format
	provider := &nirvanaCLIProvider{
		model:        "claude-opus-4-6",
		repoRoot:     repoPath,
		outputFormat: "patch_v1",
	}

	// Configure solo executor
	config := SoloConfig{
		TokenBudget:  32000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: false,
	}

	executor := NewSoloExecutor(client, provider, config)

	// Execute
	t.Log("=== GENESIS PATCH_V1 EXECUTION START ===")
	start := time.Now()
	result, err := executor.Execute(taskDesc, repoPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Execute error: %v", err)
	}

	// Write metrics
	metrics := map[string]any{
		"tokens_in":         provider.tokensIn,
		"tokens_out":        provider.tokensOut,
		"cache_creation_in": provider.cacheCreateIn,
		"cache_read_in":     provider.cacheReadIn,
		"total_cost_usd":    provider.totalCostUSD,
		"provider_calls":    provider.callCount,
		"elapsed_sec":       elapsed.Seconds(),
		"success":           result != nil && result.Success,
		"output_format":     "patch_v1",
		"error":             "",
	}
	if result != nil {
		metrics["files_modified"] = result.FilesModified
		metrics["files_created"] = result.FilesCreated
		metrics["turns"] = result.Turns
		metrics["pf_calls"] = result.PFCalls
		metrics["pack_tokens_in"] = result.TokensIn
		metrics["pack_tokens_out"] = result.TokensOut
		if result.Error != "" {
			metrics["error"] = result.Error
		}
	}
	if err != nil {
		metrics["error"] = err.Error()
	}

	os.MkdirAll("/tmp/nirvana-report", 0o755)
	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_metrics.json", metricsJSON, 0o644)

	// Save raw Angel output
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_angel_output.txt", []byte(provider.lastOutput), 0o644)
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_cli_raw.json", provider.rawCLIOutput, 0o644)

	// Print results
	t.Log("=== GENESIS PATCH_V1 METRICS ===")
	t.Logf("  tokens_in (provider):  %d", provider.tokensIn)
	t.Logf("  tokens_out (provider): %d", provider.tokensOut)
	t.Logf("  cache_create_in:       %d", provider.cacheCreateIn)
	t.Logf("  cache_read_in:         %d", provider.cacheReadIn)
	t.Logf("  total_cost_usd:        $%.6f", provider.totalCostUSD)
	t.Logf("  provider_calls:        %d", provider.callCount)
	t.Logf("  elapsed:               %s", elapsed)
	if result != nil {
		t.Logf("  success:               %v", result.Success)
		t.Logf("  files_modified:        %v", result.FilesModified)
		t.Logf("  files_created:         %v", result.FilesCreated)
		t.Logf("  pack tokens_in:        %d", result.TokensIn)
		t.Logf("  pack tokens_out:       %d", result.TokensOut)
		t.Logf("  turns:                 %d", result.Turns)
		if result.Error != "" {
			t.Logf("  error:                 %s", result.Error)
		}
	}

	// Log comparison with JSON mode if metrics exist
	jsonMetricsData, readErr := os.ReadFile("/tmp/nirvana-report/genesis_metrics.json")
	if readErr == nil {
		var jsonMetrics map[string]any
		if json.Unmarshal(jsonMetricsData, &jsonMetrics) == nil {
			if jsonOut, ok := jsonMetrics["tokens_out"].(float64); ok && jsonOut > 0 {
				pv1Out := float64(provider.tokensOut)
				savings := (1.0 - pv1Out/jsonOut) * 100.0
				t.Logf("  === COMPARISON ===")
				t.Logf("  patch_v1 output tokens: %d vs JSON output tokens: %.0f (%.1f%% savings)",
					provider.tokensOut, jsonOut, savings)
			}
		}
	}

	// Run tests in the repo
	t.Log("=== RUNNING TESTS ===")
	testCmd := exec.Command("bash", "-c",
		fmt.Sprintf("cd %s && source .venv/bin/activate && pytest -v 2>&1; deactivate", repoPath))
	testOut, testErr := testCmd.CombinedOutput()
	t.Logf("pytest output:\n%s", string(testOut))
	if testErr != nil {
		t.Logf("pytest exit error: %v", testErr)
	}

	// Save test output
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_test_output.txt", testOut, 0o644)

	// Capture git diff
	diffCmd := exec.Command("git", "-C", repoPath, "diff")
	diffOut, _ := diffCmd.Output()
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_diff.txt", diffOut, 0o644)

	diffStatCmd := exec.Command("git", "-C", repoPath, "diff", "--stat")
	diffStatOut, _ := diffStatCmd.Output()
	t.Logf("git diff --stat:\n%s", string(diffStatOut))

	// New files
	statusCmd := exec.Command("git", "-C", repoPath, "status", "--short")
	statusOut, _ := statusCmd.Output()
	t.Logf("git status:\n%s", string(statusOut))
	os.WriteFile("/tmp/nirvana-report/genesis_patch_v1_git_status.txt", statusOut, 0o644)
}

// ═══════════════════════════════════════════════════════════════════════
// TINYGRAD BENCHMARK — 3-way comparison on a real-world ML framework
// ═══════════════════════════════════════════════════════════════════════

const tinygradTaskDesc = `Add a tanhshrink activation function to tinygrad. This requires:

1) Add the tanhshrink method to the MathMixin class in tinygrad/mixin/math.py,
   after the existing tanh method (around line 460). The formula is:
   tanhshrink(x) = x - tanh(x). Follow the same docstring pattern used by
   neighboring functions like tanh, gelu, silu.

2) Add tests for tanhshrink in test/test_ops.py, in the TestOps class,
   after the existing test_mish (around line 1038). Follow the same pattern:
     helper_test_op([(45,65)], lambda x: torch.nn.functional.tanhshrink(x), Tensor.tanhshrink)
     helper_test_op([()], lambda x: torch.nn.functional.tanhshrink(x), Tensor.tanhshrink)

Run the test to verify: python -m pytest test/test_ops.py::TestOps::test_tanhshrink -v`

// TestNirvanaTinygrad runs Genesis (JSON mode) on the tinygrad codebase.
// Run with: go test -run TestNirvanaTinygrad$ -v -timeout 600s
func TestNirvanaTinygrad(t *testing.T) {
	repoPath := "/tmp/tinygrad-json"
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Skip("tinygrad-json not cloned")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}

	// Start Heaven
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewHeavenClient(ts.URL)

	provider := &nirvanaCLIProvider{
		model:    "claude-opus-4-6",
		repoRoot: repoPath,
	}

	config := SoloConfig{
		TokenBudget:  32000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: false,
	}

	executor := NewSoloExecutor(client, provider, config)

	t.Log("=== TINYGRAD GENESIS JSON EXECUTION START ===")
	start := time.Now()
	result, err := executor.Execute(tinygradTaskDesc, repoPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Execute error: %v", err)
	}

	// Write metrics
	metrics := map[string]any{
		"tokens_in":         provider.tokensIn,
		"tokens_out":        provider.tokensOut,
		"cache_creation_in": provider.cacheCreateIn,
		"cache_read_in":     provider.cacheReadIn,
		"total_cost_usd":    provider.totalCostUSD,
		"provider_calls":    provider.callCount,
		"elapsed_sec":       elapsed.Seconds(),
		"success":           result != nil && result.Success,
		"error":             "",
	}
	if result != nil {
		metrics["files_modified"] = result.FilesModified
		metrics["files_created"] = result.FilesCreated
		metrics["turns"] = result.Turns
		metrics["pf_calls"] = result.PFCalls
		metrics["pack_tokens_in"] = result.TokensIn
		metrics["pack_tokens_out"] = result.TokensOut
		if result.Error != "" {
			metrics["error"] = result.Error
		}
	}
	if err != nil {
		metrics["error"] = err.Error()
	}

	os.MkdirAll("/tmp/nirvana-report", 0o755)
	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	os.WriteFile("/tmp/nirvana-report/tinygrad_json_metrics.json", metricsJSON, 0o644)
	os.WriteFile("/tmp/nirvana-report/tinygrad_json_angel_output.txt", []byte(provider.lastOutput), 0o644)
	os.WriteFile("/tmp/nirvana-report/tinygrad_json_cli_raw.json", provider.rawCLIOutput, 0o644)

	// Print results
	t.Log("=== TINYGRAD JSON METRICS ===")
	t.Logf("  tokens_in:    %d", provider.tokensIn)
	t.Logf("  tokens_out:   %d", provider.tokensOut)
	t.Logf("  cache_create: %d", provider.cacheCreateIn)
	t.Logf("  cache_read:   %d", provider.cacheReadIn)
	t.Logf("  cost:         $%.6f", provider.totalCostUSD)
	t.Logf("  calls:        %d", provider.callCount)
	t.Logf("  elapsed:      %s", elapsed)
	if result != nil {
		t.Logf("  success:      %v", result.Success)
		t.Logf("  files_mod:    %v", result.FilesModified)
		t.Logf("  files_new:    %v", result.FilesCreated)
		t.Logf("  turns:        %d", result.Turns)
		if result.Error != "" {
			t.Logf("  error:        %s", result.Error)
		}
	}

	// Run the specific test (PYTHONPATH ensures we test THIS repo's code, not the editable install)
	t.Log("=== RUNNING TINYGRAD TEST ===")
	testCmd := exec.Command("bash", "-c",
		fmt.Sprintf("cd %s && PYTHONPATH=%s CPU=1 .venv/bin/python -m pytest test/test_ops.py::TestOps::test_tanhshrink -v 2>&1", repoPath, repoPath))
	testOut, testErr := testCmd.CombinedOutput()
	t.Logf("pytest output:\n%s", string(testOut))
	if testErr != nil {
		t.Logf("pytest exit: %v", testErr)
	}
	os.WriteFile("/tmp/nirvana-report/tinygrad_json_test_output.txt", testOut, 0o644)

	// Capture diff
	diffCmd := exec.Command("git", "-C", repoPath, "diff", "--stat")
	diffOut, _ := diffCmd.Output()
	t.Logf("git diff --stat:\n%s", string(diffOut))

	statusCmd := exec.Command("git", "-C", repoPath, "status", "--short")
	statusOut, _ := statusCmd.Output()
	t.Logf("git status:\n%s", string(statusOut))
}

// TestNirvanaTinygradPatchV1 runs Genesis (PATCH_V1 mode) on the tinygrad codebase.
// Run with: go test -run TestNirvanaTinygradPatchV1 -v -timeout 600s
func TestNirvanaTinygradPatchV1(t *testing.T) {
	repoPath := "/tmp/tinygrad-patchv1"
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Skip("tinygrad-patchv1 not cloned")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}

	// Start Heaven
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("heaven server: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewHeavenClient(ts.URL)

	provider := &nirvanaCLIProvider{
		model:        "claude-opus-4-6",
		repoRoot:     repoPath,
		outputFormat: "patch_v1",
	}

	config := SoloConfig{
		TokenBudget:  32000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: false,
	}

	executor := NewSoloExecutor(client, provider, config)

	t.Log("=== TINYGRAD GENESIS PATCH_V1 EXECUTION START ===")
	start := time.Now()
	result, err := executor.Execute(tinygradTaskDesc, repoPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Execute error: %v", err)
	}

	// Write metrics
	metrics := map[string]any{
		"tokens_in":         provider.tokensIn,
		"tokens_out":        provider.tokensOut,
		"cache_creation_in": provider.cacheCreateIn,
		"cache_read_in":     provider.cacheReadIn,
		"total_cost_usd":    provider.totalCostUSD,
		"provider_calls":    provider.callCount,
		"elapsed_sec":       elapsed.Seconds(),
		"success":           result != nil && result.Success,
		"output_format":     "patch_v1",
		"error":             "",
	}
	if result != nil {
		metrics["files_modified"] = result.FilesModified
		metrics["files_created"] = result.FilesCreated
		metrics["turns"] = result.Turns
		metrics["pf_calls"] = result.PFCalls
		metrics["pack_tokens_in"] = result.TokensIn
		metrics["pack_tokens_out"] = result.TokensOut
		if result.Error != "" {
			metrics["error"] = result.Error
		}
	}
	if err != nil {
		metrics["error"] = err.Error()
	}

	os.MkdirAll("/tmp/nirvana-report", 0o755)
	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	os.WriteFile("/tmp/nirvana-report/tinygrad_patchv1_metrics.json", metricsJSON, 0o644)
	os.WriteFile("/tmp/nirvana-report/tinygrad_patchv1_angel_output.txt", []byte(provider.lastOutput), 0o644)
	os.WriteFile("/tmp/nirvana-report/tinygrad_patchv1_cli_raw.json", provider.rawCLIOutput, 0o644)

	// Print results
	t.Log("=== TINYGRAD PATCH_V1 METRICS ===")
	t.Logf("  tokens_in:    %d", provider.tokensIn)
	t.Logf("  tokens_out:   %d", provider.tokensOut)
	t.Logf("  cache_create: %d", provider.cacheCreateIn)
	t.Logf("  cache_read:   %d", provider.cacheReadIn)
	t.Logf("  cost:         $%.6f", provider.totalCostUSD)
	t.Logf("  calls:        %d", provider.callCount)
	t.Logf("  elapsed:      %s", elapsed)
	if result != nil {
		t.Logf("  success:      %v", result.Success)
		t.Logf("  files_mod:    %v", result.FilesModified)
		t.Logf("  files_new:    %v", result.FilesCreated)
		t.Logf("  turns:        %d", result.Turns)
		if result.Error != "" {
			t.Logf("  error:        %s", result.Error)
		}
	}

	// Run the specific test (PYTHONPATH ensures we test THIS repo's code, not the editable install)
	t.Log("=== RUNNING TINYGRAD TEST ===")
	testCmd := exec.Command("bash", "-c",
		fmt.Sprintf("cd %s && PYTHONPATH=%s CPU=1 .venv/bin/python -m pytest test/test_ops.py::TestOps::test_tanhshrink -v 2>&1", repoPath, repoPath))
	testOut, testErr := testCmd.CombinedOutput()
	t.Logf("pytest output:\n%s", string(testOut))
	if testErr != nil {
		t.Logf("pytest exit: %v", testErr)
	}
	os.WriteFile("/tmp/nirvana-report/tinygrad_patchv1_test_output.txt", testOut, 0o644)

	// Capture diff
	diffCmd := exec.Command("git", "-C", repoPath, "diff", "--stat")
	diffOut, _ := diffCmd.Output()
	t.Logf("git diff --stat:\n%s", string(diffOut))

	statusCmd := exec.Command("git", "-C", repoPath, "status", "--short")
	statusOut, _ := statusCmd.Output()
	t.Logf("git status:\n%s", string(statusOut))
}
