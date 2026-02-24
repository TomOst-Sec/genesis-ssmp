package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindCodexAuth_ValidAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o700))

	authData := map[string]string{"api_key": "sk-test-codex-key-123"}
	data, _ := json.Marshal(authData)
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600))

	// Override HOME to use our temp dir
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cred, err := FindCodexAuth()
	require.NoError(t, err)
	assert.Equal(t, "sk-test-codex-key-123", cred.APIKey)
	assert.Contains(t, cred.SourcePath, "auth.json")
}

func TestFindCodexAuth_AccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o700))

	authData := map[string]string{"access_token": "tok-codex-access-456"}
	data, _ := json.Marshal(authData)
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600))

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cred, err := FindCodexAuth()
	require.NoError(t, err)
	assert.Equal(t, "tok-codex-access-456", cred.APIKey)
}

func TestFindCodexAuth_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := FindCodexAuth()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Codex CLI auth found")
}

func TestFindCodexAuth_EmptyKey(t *testing.T) {
	tmpDir := t.TempDir()
	codexDir := filepath.Join(tmpDir, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o700))

	authData := map[string]string{"api_key": ""}
	data, _ := json.Marshal(authData)
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "auth.json"), data, 0o600))

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := FindCodexAuth()
	require.Error(t, err)
}

func TestCodexAuthPaths(t *testing.T) {
	paths := CodexAuthPaths()
	require.GreaterOrEqual(t, len(paths), 1)
	for _, p := range paths {
		assert.Contains(t, p, "auth.json")
	}
}
