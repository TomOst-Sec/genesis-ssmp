package provider

import (
	"github.com/genesis-ssmp/genesis/cli/internal/auth"
	"github.com/genesis-ssmp/genesis/cli/internal/logging"
)

// FindCodexBridgeKey attempts to find a Codex CLI credential and return the API key.
// Returns the key and true if found, empty string and false otherwise.
func FindCodexBridgeKey() (string, bool) {
	cred, err := auth.FindCodexAuth()
	if err != nil {
		return "", false
	}
	if cred.APIKey == "" {
		return "", false
	}
	logging.Info("Found Codex CLI credentials", "source", cred.SourcePath)
	return cred.APIKey, true
}

// FindGeminiBridgeKey attempts to find a Gemini CLI credential and return the API key.
// Returns the key and true if found, empty string and false otherwise.
func FindGeminiBridgeKey() (string, bool) {
	cred, err := auth.FindGeminiAuth()
	if err != nil {
		return "", false
	}
	if cred.APIKey == "" {
		return "", false
	}
	logging.Info("Found Gemini CLI credentials", "source", cred.SourcePath)
	return cred.APIKey, true
}
