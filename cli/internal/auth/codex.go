package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CodexCredential holds a credential imported from the OpenAI Codex CLI.
type CodexCredential struct {
	APIKey      string // the API key or access token
	SourcePath  string // file it was read from
}

// codexAuthFile represents the JSON structure of ~/.codex/auth.json.
type codexAuthFile struct {
	APIKey      string          `json:"api_key"`
	AccessToken string          `json:"access_token"`
	Token       string          `json:"token"`
	Tokens      codexTokenBlock `json:"tokens"`
	OpenAIKey   string          `json:"OPENAI_API_KEY"`
}

type codexTokenBlock struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

// FindCodexAuth searches standard locations for Codex CLI auth credentials.
// Returns the first valid credential found, or an error.
func FindCodexAuth() (*CodexCredential, error) {
	for _, path := range CodexAuthPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var auth codexAuthFile
		if err := json.Unmarshal(data, &auth); err != nil {
			continue
		}

		key := auth.OpenAIKey
		if key == "" {
			key = auth.APIKey
		}
		if key == "" {
			key = auth.AccessToken
		}
		if key == "" {
			key = auth.Tokens.AccessToken
		}
		if key == "" {
			key = auth.Token
		}
		if key == "" {
			continue
		}

		return &CodexCredential{
			APIKey:     key,
			SourcePath: path,
		}, nil
	}

	return nil, fmt.Errorf("no Codex CLI auth found in standard locations")
}

// CodexAuthPaths returns all paths where Codex CLI auth might be stored.
func CodexAuthPaths() []string {
	var paths []string

	homeDir, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(homeDir, ".codex", "auth.json"))
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "codex", "auth.json"))
	} else if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, ".config", "codex", "auth.json"))
	}

	return paths
}
