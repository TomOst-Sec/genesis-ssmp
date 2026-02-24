package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BridgeConfig holds configuration for a CLI bridge auth method.
type BridgeConfig struct {
	CLIName      string   // primary binary: "codex", "gh", "claude"
	CLIAltNames  []string // alternative names: ["claude-code"]
	LoginArgs    []string // e.g. ["login"] or ["auth", "login", "--web"]
	TokenArgs    []string // e.g. ["auth", "token"] — run to get token from stdout
	CachePaths   []string // e.g. ["~/.codex/auth.json"] — expand ~ and $XDG
	CacheParser  string   // "codex_json", "gh_token", "none"
	SupportLevel string   // "supported", "experimental", "blocked"
	SupportNote  string   // shown in UI
	Label        string   // display: "ChatGPT (Bridge: Codex)"
}

// BridgeInfo is the result of detecting a bridge.
type BridgeInfo struct {
	CLIPath    string // resolved path, "" if not found
	CLIFound   bool
	CacheFound bool
	CachePath  string
	Available  bool // true if CLI found OR cache found
}

// BridgeResult is the result of importing a token via bridge.
type BridgeResult struct {
	Token      string
	Source     string // "cache_file", "cli_token_cmd"
	SourcePath string
}

// DetectBridge checks if a bridge CLI is installed and/or cached tokens exist.
func DetectBridge(cfg *BridgeConfig) *BridgeInfo {
	if cfg == nil {
		return &BridgeInfo{}
	}

	info := &BridgeInfo{}

	// Find CLI binary
	names := append([]string{cfg.CLIName}, cfg.CLIAltNames...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			info.CLIPath = path
			info.CLIFound = true
			break
		}
	}

	// Check cache paths
	for _, raw := range cfg.CachePaths {
		expanded := expandPath(raw)
		if _, err := os.Stat(expanded); err == nil {
			info.CacheFound = true
			info.CachePath = expanded
			break
		}
	}

	info.Available = info.CLIFound || info.CacheFound
	return info
}

// ImportBridgeToken reads a token from cache files or a CLI token command.
// It does NOT invoke login — only reads existing credentials.
func ImportBridgeToken(ctx context.Context, cfg *BridgeConfig) (*BridgeResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no bridge config")
	}

	if cfg.CacheParser == "none" {
		return nil, fmt.Errorf("token import not available for this bridge")
	}

	// Strategy 1: CLI token command (e.g., `gh auth token`)
	if len(cfg.TokenArgs) > 0 {
		result, err := importViaCLICmd(ctx, cfg)
		if err == nil {
			return result, nil
		}
		// Fall through to cache files
	}

	// Strategy 2: Parse cache files
	switch cfg.CacheParser {
	case "codex_json":
		cred, err := FindCodexAuth()
		if err != nil {
			return nil, fmt.Errorf("codex cache: %w", err)
		}
		return &BridgeResult{
			Token:      cred.APIKey,
			Source:     "cache_file",
			SourcePath: cred.SourcePath,
		}, nil

	case "gh_token":
		token, source, err := FindGitHubToken()
		if err != nil {
			return nil, fmt.Errorf("github token: %w", err)
		}
		return &BridgeResult{
			Token:      token,
			Source:     "cache_file",
			SourcePath: source,
		}, nil

	case "claude_json":
		cred, err := FindClaudeAuth()
		if err != nil {
			return nil, fmt.Errorf("claude cache: %w", err)
		}
		return &BridgeResult{
			Token:      cred.Token,
			Source:     "cache_file",
			SourcePath: cred.SourcePath,
		}, nil

	default:
		return nil, fmt.Errorf("unknown cache parser: %s", cfg.CacheParser)
	}
}

// BridgeLoginCmd returns an *exec.Cmd for the bridge CLI's login command.
// The caller should use tea.Exec() to run it, which suspends the TUI and
// gives the CLI full terminal control.
func BridgeLoginCmd(cfg *BridgeConfig) (*exec.Cmd, error) {
	if cfg == nil || len(cfg.LoginArgs) == 0 {
		return nil, fmt.Errorf("no login command configured")
	}

	// Resolve CLI path
	cliPath := ""
	names := append([]string{cfg.CLIName}, cfg.CLIAltNames...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			cliPath = path
			break
		}
	}
	if cliPath == "" {
		return nil, fmt.Errorf("%s CLI not found in PATH", cfg.CLIName)
	}

	cmd := exec.Command(cliPath, cfg.LoginArgs...)
	return cmd, nil
}

// importViaCLICmd runs a CLI command and captures its stdout as a token.
func importViaCLICmd(ctx context.Context, cfg *BridgeConfig) (*BridgeResult, error) {
	cliPath := ""
	names := append([]string{cfg.CLIName}, cfg.CLIAltNames...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			cliPath = path
			break
		}
	}
	if cliPath == "" {
		return nil, fmt.Errorf("%s not found in PATH", cfg.CLIName)
	}

	cmd := exec.CommandContext(ctx, cliPath, cfg.TokenArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run %s %s: %w", cfg.CLIName, strings.Join(cfg.TokenArgs, " "), err)
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return nil, fmt.Errorf("%s returned empty token", cfg.CLIName)
	}

	return &BridgeResult{
		Token:      token,
		Source:     "cli_token_cmd",
		SourcePath: fmt.Sprintf("%s %s", cfg.CLIName, strings.Join(cfg.TokenArgs, " ")),
	}, nil
}

// expandPath expands ~ and $XDG_CONFIG_HOME in a path.
func expandPath(raw string) string {
	if strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, raw[2:])
		}
	}
	raw = os.ExpandEnv(raw)
	return raw
}
