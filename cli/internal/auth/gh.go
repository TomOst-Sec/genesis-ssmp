package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindGitHubToken reads a GitHub token from gh CLI or standard config files.
// Returns token, source description, error.
func FindGitHubToken() (string, string, error) {
	// Strategy 1: `gh auth token` — most reliable
	if ghPath, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(ghPath, "auth", "token").Output()
		if err == nil {
			token := strings.TrimSpace(string(out))
			if token != "" {
				return token, "gh auth token", nil
			}
		}
	}

	// Strategy 2: GitHub Copilot config files
	configDir := ghConfigDir()

	// Try github-copilot hosts.json and apps.json
	copilotPaths := []string{
		filepath.Join(configDir, "github-copilot", "hosts.json"),
		filepath.Join(configDir, "github-copilot", "apps.json"),
	}

	for _, path := range copilotPaths {
		token, err := parseGitHubHostsJSON(path)
		if err == nil && token != "" {
			return token, path, nil
		}
	}

	// Strategy 3: gh CLI hosts.yml (YAML, but oauth_token is on its own line)
	ghHostsPath := filepath.Join(configDir, "gh", "hosts.yml")
	token, err := parseGHHostsYAML(ghHostsPath)
	if err == nil && token != "" {
		return token, ghHostsPath, nil
	}

	return "", "", fmt.Errorf("GitHub token not found")
}

// ghConfigDir returns the platform-appropriate config directory.
func ghConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local
		}
		return filepath.Join(os.Getenv("HOME"), "AppData", "Local")
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

// parseGitHubHostsJSON reads a GitHub Copilot hosts.json or apps.json file
// and extracts the oauth_token for github.com.
func parseGitHubHostsJSON(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var hosts map[string]map[string]interface{}
	if err := json.Unmarshal(data, &hosts); err != nil {
		return "", err
	}

	for key, value := range hosts {
		if strings.Contains(key, "github.com") {
			if token, ok := value["oauth_token"].(string); ok && token != "" {
				return token, nil
			}
		}
	}
	return "", fmt.Errorf("no github.com token in %s", path)
}

// parseGHHostsYAML does a simple line-based parse of gh's hosts.yml.
// We avoid a YAML dependency by looking for the oauth_token line after github.com.
func parseGHHostsYAML(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	inGitHub := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Top-level host entries are not indented
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(trimmed, "github.com") {
			inGitHub = true
			continue
		}
		// Another top-level entry — stop looking
		if inGitHub && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
			break
		}
		if inGitHub && strings.Contains(trimmed, "oauth_token:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				token := strings.TrimSpace(parts[1])
				if token != "" {
					return token, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no github.com token in %s", path)
}
