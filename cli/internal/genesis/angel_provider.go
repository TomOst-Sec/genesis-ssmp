package genesis

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/genesis-ssmp/genesis/god"
)

const angelSystemPrompt = `You are a Genesis Angel — an AI code editor that receives mission packs and produces Edit IR (structured code modifications).

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

// AnthropicAngelProvider implements god.Provider by routing through the Claude
// Code CLI, which handles OAuth token authentication.
type AnthropicAngelProvider struct {
	model     string // Anthropic API model ID e.g. "claude-opus-4-6"
	repoRoot  string
	cliPath   string
	LastUsage *god.CLITokenUsage // real token usage from last Claude CLI call
}

// NewAnthropicAngelProvider creates a provider that sends mission packs to Claude
// via the Claude Code CLI proxy.
func NewAnthropicAngelProvider(model, repoRoot string) *AnthropicAngelProvider {
	cliPath, _ := exec.LookPath("claude")
	return &AnthropicAngelProvider{
		model:    model,
		repoRoot: repoRoot,
		cliPath:  cliPath,
	}
}

// claudeJSONResult matches Claude Code's --output-format json structure.
type claudeJSONResult struct {
	Type         string `json:"type"`
	Result       string `json:"result"`
	IsError      bool   `json:"is_error"`
	DurationMS   int64  `json:"duration_ms"`
	NumTurns     int    `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage map[string]struct {
		InputTokens              int64   `json:"inputTokens"`
		OutputTokens             int64   `json:"outputTokens"`
		CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
		CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
		CostUSD                  float64 `json:"costUSD"`
	} `json:"modelUsage"`
}

// Send implements god.Provider. It converts a MissionPack into a prompt,
// sends it through the Claude Code CLI, and returns AngelResponse JSON bytes.
func (p *AnthropicAngelProvider) Send(pack *god.MissionPack) ([]byte, error) {
	if p.cliPath == "" {
		return nil, fmt.Errorf("claude CLI not found in PATH — install Claude Code")
	}

	// 1. Build prompt from pack
	prompt := p.buildUserMessage(pack)

	// 2. Call Claude Code CLI
	args := []string{
		"-p",
		"--output-format", "json",
		"--model", p.model,
		"--no-session-persistence",
		"--system-prompt", angelSystemPrompt,
	}

	cmd := exec.Command(p.cliPath, args...)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude CLI (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI: %w", err)
	}

	// 3. Parse Claude Code JSON result
	var result claudeJSONResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse claude output: %w", err)
	}

	// Capture real token usage from Claude CLI response
	usage := &god.CLITokenUsage{
		InputTokens:              result.Usage.InputTokens,
		OutputTokens:             result.Usage.OutputTokens,
		CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
		CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
		TotalCostUSD:             result.TotalCostUSD,
		NumTurns:                 result.NumTurns,
		DurationMS:               result.DurationMS,
	}
	// If modelUsage available, prefer cumulative per-model totals
	for _, mu := range result.ModelUsage {
		usage.InputTokens += mu.InputTokens
		usage.OutputTokens += mu.OutputTokens
		usage.CacheReadInputTokens += mu.CacheReadInputTokens
		usage.CacheCreationInputTokens += mu.CacheCreationInputTokens
	}
	// If modelUsage was present, subtract the top-level (last-turn) values to avoid double-counting
	if len(result.ModelUsage) > 0 {
		usage.InputTokens -= result.Usage.InputTokens
		usage.OutputTokens -= result.Usage.OutputTokens
		usage.CacheReadInputTokens -= result.Usage.CacheReadInputTokens
		usage.CacheCreationInputTokens -= result.Usage.CacheCreationInputTokens
	}
	p.LastUsage = usage

	if result.IsError {
		return nil, fmt.Errorf("claude error: %s", result.Result)
	}

	text := result.Result
	if text == "" {
		return nil, fmt.Errorf("claude returned empty response")
	}

	// 4. Strip markdown code fences and extract JSON
	text = stripCodeFences(text)
	text = extractJSON(text)

	// 5. Fix anchor hashes
	fixed, err := p.fixAnchorHashes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("fix anchor hashes: %w", err)
	}

	return fixed, nil
}

// buildUserMessage constructs the prompt from a MissionPack.
func (p *AnthropicAngelProvider) buildUserMessage(pack *god.MissionPack) string {
	var sb strings.Builder

	// Pack header (contains instructions, constraints, PF playbook)
	sb.WriteString(pack.Header)
	sb.WriteString("\n\n")

	// Mission details
	missionJSON, _ := json.MarshalIndent(pack.Mission, "", "  ")
	sb.WriteString("MISSION:\n")
	sb.WriteString(string(missionJSON))
	sb.WriteString("\n\n")

	// Inline shards (symbol metadata from IR)
	if len(pack.InlineShards) > 0 {
		sb.WriteString("CODE CONTEXT (from IR index):\n")
		for _, shard := range pack.InlineShards {
			fmt.Fprintf(&sb, "--- %s [%s] ---\n", shard.Kind, shard.BlobID)
			sb.WriteString(string(shard.Content))
			sb.WriteString("\n\n")
		}
	}

	// Read and include actual source files
	files := p.collectFilePaths(pack)
	if len(files) > 0 {
		sb.WriteString("SOURCE FILES:\n")
		for _, path := range files {
			absPath := filepath.Join(p.repoRoot, path)
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			fmt.Fprintf(&sb, "--- %s ---\n", path)
			sb.WriteString(string(data))
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("Execute this mission. Return ONLY the AngelResponse JSON.")
	return sb.String()
}

// collectFilePaths extracts unique file paths from the mission pack.
func (p *AnthropicAngelProvider) collectFilePaths(pack *god.MissionPack) []string {
	seen := make(map[string]bool)
	var paths []string

	// From inline shards metadata
	for _, shard := range pack.InlineShards {
		if shard.Meta != nil {
			if path, ok := shard.Meta["path"].(string); ok && path != "" && !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
		// Also try to extract path from shard content
		var content map[string]any
		if err := json.Unmarshal(shard.Content, &content); err == nil {
			if path, ok := content["path"].(string); ok && path != "" && !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}

	// From mission scopes
	for _, scope := range pack.Mission.Scopes {
		if scope.ScopeType == "file" && !seen[scope.ScopeValue] {
			seen[scope.ScopeValue] = true
			paths = append(paths, scope.ScopeValue)
		}
	}

	// If no paths found, scan the repo for common source files
	if len(paths) == 0 {
		paths = p.scanSourceFiles()
	}

	return paths
}

// scanSourceFiles finds Python source files in the repo.
func (p *AnthropicAngelProvider) scanSourceFiles() []string {
	var files []string
	filepath.Walk(p.repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".genesis" || base == "__pycache__" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".py" || ext == ".md" {
			rel, _ := filepath.Rel(p.repoRoot, path)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// fixAnchorHashes parses an AngelResponse and computes correct anchor hashes.
func (p *AnthropicAngelProvider) fixAnchorHashes(raw []byte) ([]byte, error) {
	var resp god.AngelResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse angel response: %w", err)
	}

	if resp.EditIR == nil {
		return raw, nil
	}

	for i, op := range resp.EditIR.Ops {
		switch op.Op {
		case "add_file":
			resp.EditIR.Ops[i].AnchorHash = god.ComputeAnchorHash(nil, 0, 0)

		case "replace_span", "delete_span":
			if len(op.Lines) != 2 {
				continue
			}
			absPath := filepath.Join(p.repoRoot, op.Path)
			lines, err := readLines(absPath)
			if err != nil {
				continue
			}
			if op.Lines[1] <= len(lines) {
				resp.EditIR.Ops[i].AnchorHash = god.ComputeAnchorHash(lines, op.Lines[0], op.Lines[1])
			}

		case "insert_after_symbol":
			absPath := filepath.Join(p.repoRoot, op.Path)
			lines, err := readLines(absPath)
			if err != nil {
				continue
			}
			for j, line := range lines {
				if strings.Contains(line, op.Symbol) {
					resp.EditIR.Ops[i].AnchorHash = god.ComputeAnchorHash(lines, j+1, j+1)
					break
				}
			}
		}
	}

	return json.Marshal(resp)
}

// readLines reads a file and splits into lines (matching god.readFileLines behavior).
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSuffix(string(data), "\n")
	return strings.Split(content, "\n"), nil
}

// extractJSON finds the first top-level JSON object in a string that may
// contain surrounding conversational text from the LLM.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}
	// Find matching closing brace
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		case '"':
			// Skip string contents (handles escaped quotes)
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return s[start:]
}

// CLIUsage returns the real token usage from the last Claude CLI call.
// Implements god.CLIUsageProvider.
func (p *AnthropicAngelProvider) CLIUsage() *god.CLITokenUsage {
	return p.LastUsage
}

// stripCodeFences removes markdown code fences from LLM output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if after, found := strings.CutPrefix(s, "```json"); found {
		s = after
	} else if after, found := strings.CutPrefix(s, "```"); found {
		s = after
	}
	if after, found := strings.CutSuffix(s, "```"); found {
		s = after
	}
	return strings.TrimSpace(s)
}
