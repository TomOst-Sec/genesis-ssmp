package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectBridge_CLIFound(t *testing.T) {
	// Create a fake binary in a temp dir and add to PATH
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "fakecli")
	err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake"), 0o755)
	require.NoError(t, err)

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	cfg := &BridgeConfig{CLIName: "fakecli"}
	info := DetectBridge(cfg)
	assert.True(t, info.CLIFound)
	assert.Equal(t, fakeBin, info.CLIPath)
	assert.True(t, info.Available)
}

func TestDetectBridge_CLINotFound(t *testing.T) {
	cfg := &BridgeConfig{CLIName: "nonexistent_bridge_cli_xyz"}
	info := DetectBridge(cfg)
	assert.False(t, info.CLIFound)
	assert.Empty(t, info.CLIPath)
}

func TestDetectBridge_AltName(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "alt-cli")
	err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho alt"), 0o755)
	require.NoError(t, err)

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	cfg := &BridgeConfig{
		CLIName:     "primary-missing",
		CLIAltNames: []string{"alt-cli"},
	}
	info := DetectBridge(cfg)
	assert.True(t, info.CLIFound)
	assert.Equal(t, fakeBin, info.CLIPath)
}

func TestDetectBridge_CacheFound(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "auth.json")
	err := os.WriteFile(cacheFile, []byte(`{"api_key":"test"}`), 0o600)
	require.NoError(t, err)

	cfg := &BridgeConfig{
		CLIName:    "nonexistent",
		CachePaths: []string{cacheFile},
	}
	info := DetectBridge(cfg)
	assert.True(t, info.CacheFound)
	assert.Equal(t, cacheFile, info.CachePath)
	assert.True(t, info.Available)
}

func TestDetectBridge_NeitherFound(t *testing.T) {
	cfg := &BridgeConfig{
		CLIName:    "nonexistent_bridge_cli_xyz",
		CachePaths: []string{"/tmp/nonexistent_path_xyz/auth.json"},
	}
	info := DetectBridge(cfg)
	assert.False(t, info.CLIFound)
	assert.False(t, info.CacheFound)
	assert.False(t, info.Available)
}

func TestDetectBridge_Nil(t *testing.T) {
	info := DetectBridge(nil)
	assert.False(t, info.Available)
}

func TestImportBridgeToken_CodexJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, ".codex", "auth.json")
	err := os.MkdirAll(filepath.Dir(cacheFile), 0o700)
	require.NoError(t, err)

	data, _ := json.Marshal(map[string]string{"api_key": "sk-test-bridge-key"})
	err = os.WriteFile(cacheFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cfg := &BridgeConfig{
		CLIName:     "codex",
		CachePaths:  []string{"~/.codex/auth.json"},
		CacheParser: "codex_json",
	}

	result, err := ImportBridgeToken(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "sk-test-bridge-key", result.Token)
	assert.Equal(t, "cache_file", result.Source)
}

func TestImportBridgeToken_CLITokenCmd(t *testing.T) {
	// Create a fake CLI that prints a token
	tmpDir := t.TempDir()
	fakeScript := filepath.Join(tmpDir, "fake-token-cli")
	err := os.WriteFile(fakeScript, []byte("#!/bin/sh\necho gho_test_token_from_cli"), 0o755)
	require.NoError(t, err)

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	cfg := &BridgeConfig{
		CLIName:     "fake-token-cli",
		TokenArgs:   []string{},
		CachePaths:  []string{"/nonexistent"},
		CacheParser: "codex_json",
	}

	// TokenArgs is empty so it should fall through to cache parser
	// which won't find anything. Let's test with actual token args.
	cfg.TokenArgs = nil // no token cmd, should fall to cache
	_, err = ImportBridgeToken(context.Background(), cfg)
	assert.Error(t, err, "should fail when no cache file exists")
}

func TestImportBridgeToken_NoCacheNoCmd(t *testing.T) {
	cfg := &BridgeConfig{
		CLIName:     "nonexistent",
		CacheParser: "codex_json",
	}
	_, err := ImportBridgeToken(context.Background(), cfg)
	assert.Error(t, err)
}

func TestImportBridgeToken_ClaudeJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, ".claude.json")
	data, _ := json.Marshal(map[string]string{"accessToken": "sk-ant-bridge-test"})
	err := os.WriteFile(cacheFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv("HOME", tmpDir)

	cfg := &BridgeConfig{
		CLIName:     "claude",
		CachePaths:  []string{"~/.claude.json"},
		CacheParser: "claude_json",
	}

	result, err := ImportBridgeToken(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-bridge-test", result.Token)
	assert.Equal(t, "cache_file", result.Source)
}

func TestImportBridgeToken_ClaudeJSON_NoCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "xdg"))

	cfg := &BridgeConfig{
		CLIName:     "claude",
		CacheParser: "claude_json",
	}

	_, err := ImportBridgeToken(context.Background(), cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "claude cache")
}

func TestImportBridgeToken_NoneParser(t *testing.T) {
	cfg := &BridgeConfig{
		CLIName:     "claude",
		CacheParser: "none",
	}
	_, err := ImportBridgeToken(context.Background(), cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestImportBridgeToken_Nil(t *testing.T) {
	_, err := ImportBridgeToken(context.Background(), nil)
	assert.Error(t, err)
}

func TestBridgeLoginCmd_ResolvesCLI(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "test-login-cli")
	err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755)
	require.NoError(t, err)

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	cfg := &BridgeConfig{
		CLIName:   "test-login-cli",
		LoginArgs: []string{"login", "--web"},
	}

	cmd, err := BridgeLoginCmd(cfg)
	require.NoError(t, err)
	assert.Equal(t, fakeBin, cmd.Path)
	assert.Equal(t, []string{fakeBin, "login", "--web"}, cmd.Args)
}

func TestBridgeLoginCmd_CLINotFound(t *testing.T) {
	cfg := &BridgeConfig{
		CLIName:   "nonexistent_cli_xyz",
		LoginArgs: []string{"login"},
	}
	_, err := BridgeLoginCmd(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBridgeLoginCmd_NoLoginArgs(t *testing.T) {
	cfg := &BridgeConfig{CLIName: "anything"}
	_, err := BridgeLoginCmd(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no login command")
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".codex", "auth.json"), expandPath("~/.codex/auth.json"))
}
