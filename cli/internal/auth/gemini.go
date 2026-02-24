package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GeminiCredential holds a credential imported from the Gemini CLI.
type GeminiCredential struct {
	APIKey     string // the API key or access token
	SourcePath string // file it was read from
}

// geminiAuthFile represents the JSON structure of Gemini CLI auth files.
type geminiAuthFile struct {
	APIKey      string `json:"api_key"`
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
	// Google OAuth format
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	Type         string `json:"type"`
}

// FindGeminiAuth searches standard locations for Gemini CLI auth credentials.
// Returns the first valid credential found, or an error.
func FindGeminiAuth() (*GeminiCredential, error) {
	for _, path := range GeminiAuthPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var auth geminiAuthFile
		if err := json.Unmarshal(data, &auth); err != nil {
			continue
		}

		key := auth.APIKey
		if key == "" {
			key = auth.AccessToken
		}
		if key == "" {
			key = auth.Token
		}
		if key == "" {
			continue
		}

		return &GeminiCredential{
			APIKey:     key,
			SourcePath: path,
		}, nil
	}

	return nil, fmt.Errorf("no Gemini CLI auth found in standard locations")
}

// GeminiAuthPaths returns all paths where Gemini CLI auth might be stored.
func GeminiAuthPaths() []string {
	var paths []string

	homeDir, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths,
			filepath.Join(homeDir, ".config", "gemini", "auth.json"),
			filepath.Join(homeDir, ".gemini", "credentials.json"),
			filepath.Join(homeDir, ".config", "google-cloud", "application_default_credentials.json"),
		)
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths,
			filepath.Join(xdg, "gemini", "auth.json"),
			filepath.Join(xdg, "gemini", "credentials.json"),
		)
	}

	return paths
}
