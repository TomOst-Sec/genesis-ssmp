package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindClaudeAuth_TopLevelAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	data, _ := json.Marshal(map[string]string{"accessToken": "sk-ant-test-token"})
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test-token", cred.Token)
	assert.Contains(t, cred.SourcePath, ".claude.json")
}

func TestFindClaudeAuth_TopLevelSessionKey(t *testing.T) {
	tmpDir := t.TempDir()
	data, _ := json.Marshal(map[string]string{"sessionKey": "sk-session-xyz"})
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "sk-session-xyz", cred.Token)
}

func TestFindClaudeAuth_TopLevelAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	data, _ := json.Marshal(map[string]string{"apiKey": "sk-ant-api-key-123"})
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-api-key-123", cred.Token)
}

func TestFindClaudeAuth_NestedProviderBlock(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte(`{"claude.ai": {"accessToken": "nested-token-abc"}}`)
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "nested-token-abc", cred.Token)
}

func TestFindClaudeAuth_TopLevelTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte(`{"accessToken": "top-level", "claude.ai": {"accessToken": "nested"}}`)
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "top-level", cred.Token)
}

func TestFindClaudeAuth_XDGConfigPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Don't create ~/.claude.json so it falls through to XDG
	xdgDir := filepath.Join(tmpDir, "xdg")
	credDir := filepath.Join(xdgDir, "claude")
	require.NoError(t, os.MkdirAll(credDir, 0o700))

	data, _ := json.Marshal(map[string]string{"accessToken": "xdg-token"})
	err := os.WriteFile(filepath.Join(credDir, "credentials.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	cred, err := FindClaudeAuth()
	require.NoError(t, err)
	assert.Equal(t, "xdg-token", cred.Token)
	assert.Contains(t, cred.SourcePath, "credentials.json")
}

func TestFindClaudeAuth_EmptyToken(t *testing.T) {
	tmpDir := t.TempDir()
	data, _ := json.Marshal(map[string]string{"accessToken": "", "apiKey": ""})
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	_, err = FindClaudeAuth()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no Claude CLI credentials found")
}

func TestFindClaudeAuth_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), []byte("not json"), 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	_, err = FindClaudeAuth()
	assert.Error(t, err)
}

func TestFindClaudeAuth_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "nonexistent_xdg"))

	_, err := FindClaudeAuth()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no Claude CLI credentials found")
}

func TestFindClaudeAuth_PermissionDenied(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, ".claude.json")
	data, _ := json.Marshal(map[string]string{"accessToken": "secret"})
	err := os.WriteFile(credFile, data, 0o000)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	// Should fail gracefully (skip unreadable files)
	_, err = FindClaudeAuth()
	assert.Error(t, err)
}

func TestClaudeAuthPaths_ContainsExpectedPaths(t *testing.T) {
	paths := ClaudeAuthPaths()
	assert.NotEmpty(t, paths)

	// Should include ~/.claude.json
	found := false
	for _, p := range paths {
		if filepath.Base(p) == ".claude.json" {
			found = true
			break
		}
	}
	assert.True(t, found, "should include .claude.json path")
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "a", firstNonEmpty("a", "b", "c"))
	assert.Equal(t, "b", firstNonEmpty("", "b", "c"))
	assert.Equal(t, "c", firstNonEmpty("", "", "c"))
	assert.Equal(t, "", firstNonEmpty("", "", ""))
}
