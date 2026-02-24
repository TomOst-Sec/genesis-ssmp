package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindGitHubToken_GHAuthToken(t *testing.T) {
	// Create a fake `gh` binary that outputs a token
	tmpDir := t.TempDir()
	fakeGH := filepath.Join(tmpDir, "gh")
	// The script checks if args contain "auth token" and prints the token
	script := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
    echo "gho_fake_gh_token_12345"
    exit 0
fi
exit 1
`
	err := os.WriteFile(fakeGH, []byte(script), 0o755)
	require.NoError(t, err)

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir()) // Empty home so no config files

	token, source, err := FindGitHubToken()
	require.NoError(t, err)
	assert.Equal(t, "gho_fake_gh_token_12345", token)
	assert.Equal(t, "gh auth token", source)
}

func TestFindGitHubToken_HostsJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// No gh binary in PATH
	t.Setenv("PATH", tmpDir) // empty PATH except tmpDir (no gh)
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Create github-copilot hosts.json
	hostsDir := filepath.Join(tmpDir, ".config", "github-copilot")
	err := os.MkdirAll(hostsDir, 0o700)
	require.NoError(t, err)

	hosts := map[string]map[string]string{
		"github.com": {
			"oauth_token": "gho_from_copilot_hosts",
		},
	}
	data, _ := json.Marshal(hosts)
	err = os.WriteFile(filepath.Join(hostsDir, "hosts.json"), data, 0o600)
	require.NoError(t, err)

	token, source, err := FindGitHubToken()
	require.NoError(t, err)
	assert.Equal(t, "gho_from_copilot_hosts", token)
	assert.Contains(t, source, "hosts.json")
}

func TestFindGitHubToken_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir) // no gh
	t.Setenv("HOME", tmpDir) // no config files
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GITHUB_TOKEN", "") // ensure not set

	_, _, err := FindGitHubToken()
	assert.Error(t, err)
}

func TestParseGHHostsYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlContent := `github.com:
    oauth_token: gho_from_yaml_123
    user: testuser
    git_protocol: https
`
	yamlFile := filepath.Join(tmpDir, "hosts.yml")
	err := os.WriteFile(yamlFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	token, err := parseGHHostsYAML(yamlFile)
	require.NoError(t, err)
	assert.Equal(t, "gho_from_yaml_123", token)
}

func TestParseGHHostsYAML_Missing(t *testing.T) {
	_, err := parseGHHostsYAML("/nonexistent/file")
	assert.Error(t, err)
}
