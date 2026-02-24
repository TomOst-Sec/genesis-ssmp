package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ClaudeCredential holds a credential imported from the Claude CLI.
type ClaudeCredential struct {
	Token      string // the access token, session key, or API key
	SourcePath string // file it was read from
	ExpiresAt  int64  // Unix milliseconds, 0 if unknown
}

// IsExpired returns true if the credential has a known expiry that has passed.
func (c *ClaudeCredential) IsExpired() bool {
	if c.ExpiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli() >= c.ExpiresAt
}

// claudeAuthFile represents possible JSON fields in Claude CLI credential files.
// The Claude CLI stores tokens in various formats; we look for the first non-empty field.
type claudeAuthFile struct {
	// OAuth / session tokens
	AccessToken  string `json:"accessToken"`
	SessionKey   string `json:"sessionKey"`
	APIKey       string `json:"apiKey"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`

	// Nested: sometimes under a provider key
	ClaudeAI *claudeProviderBlock `json:"claude.ai"`

	// Claude Code OAuth credentials (~/.claude/.credentials.json)
	ClaudeAIOAuth *claudeOAuthBlock `json:"claudeAiOauth"`
}

type claudeOAuthBlock struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

type claudeProviderBlock struct {
	AccessToken string `json:"accessToken"`
	SessionKey  string `json:"sessionKey"`
	APIKey      string `json:"apiKey"`
	Token       string `json:"token"`
}

// FindClaudeAuth searches standard locations for Claude CLI auth credentials.
// Returns the first valid credential found, or an error.
func FindClaudeAuth() (*ClaudeCredential, error) {
	for _, path := range ClaudeAuthPaths() {
		cred, err := parseClaudeAuthFile(path)
		if err != nil {
			continue
		}
		if cred != nil {
			return cred, nil
		}
	}

	return nil, fmt.Errorf("no Claude CLI credentials found in standard locations")
}

// parseClaudeAuthFile attempts to read and extract a token from a single file.
func parseClaudeAuthFile(path string) (*ClaudeCredential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var auth claudeAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}

	// Check top-level fields
	token := firstNonEmpty(
		auth.AccessToken,
		auth.SessionKey,
		auth.APIKey,
		auth.Token,
	)

	// Check nested provider block
	if token == "" && auth.ClaudeAI != nil {
		token = firstNonEmpty(
			auth.ClaudeAI.AccessToken,
			auth.ClaudeAI.SessionKey,
			auth.ClaudeAI.APIKey,
			auth.ClaudeAI.Token,
		)
	}

	// Check Claude Code OAuth block (~/.claude/.credentials.json)
	var expiresAt int64
	if token == "" && auth.ClaudeAIOAuth != nil {
		token = auth.ClaudeAIOAuth.AccessToken
		expiresAt = auth.ClaudeAIOAuth.ExpiresAt
	}

	if token == "" {
		return nil, fmt.Errorf("no token field found in %s", path)
	}

	cred := &ClaudeCredential{
		Token:      token,
		SourcePath: path,
		ExpiresAt:  expiresAt,
	}

	// Skip expired OAuth tokens
	if cred.IsExpired() {
		return nil, fmt.Errorf("token in %s is expired", path)
	}

	return cred, nil
}

// ClaudeAuthPaths returns all paths where Claude CLI credentials might be stored.
// These are read-only lookups — we never write to another CLI's directories.
func ClaudeAuthPaths() []string {
	var paths []string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return paths
	}

	// Claude Code OAuth credentials (primary location)
	paths = append(paths, filepath.Join(homeDir, ".claude", ".credentials.json"))

	// Claude Code / claude-code stores credentials here
	paths = append(paths, filepath.Join(homeDir, ".claude.json"))

	// XDG config locations
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(homeDir, ".config")
	}

	// Claude CLI config directory
	paths = append(paths, filepath.Join(xdgConfig, "claude", "credentials.json"))
	paths = append(paths, filepath.Join(xdgConfig, "claude", "auth.json"))
	paths = append(paths, filepath.Join(xdgConfig, "claude", "config.json"))

	// Anthropic SDK / CLI directory
	paths = append(paths, filepath.Join(homeDir, ".anthropic", "auth.json"))
	paths = append(paths, filepath.Join(homeDir, ".anthropic", "credentials.json"))

	// macOS: ~/Library/Application Support/Claude/
	if runtime.GOOS == "darwin" {
		paths = append(paths, filepath.Join(homeDir, "Library", "Application Support", "Claude", "credentials.json"))
		paths = append(paths, filepath.Join(homeDir, "Library", "Application Support", "Claude", "auth.json"))
	}

	return paths
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
