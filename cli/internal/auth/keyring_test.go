package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStoreAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	// Override HOME so credentialDir uses temp path
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := storeInFile("test-provider", "sk-test-secret-key")
	require.NoError(t, err)

	loaded, err := loadFromFile("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-secret-key", loaded)
}

func TestFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	err := storeInFile("perm-test", "secret")
	require.NoError(t, err)

	path := filepath.Join(tmpDir, ".config", "genesis", "credentials", "perm-test.key")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "credential file should be 0600")

	// Check directory permissions
	dirInfo, err := os.Stat(filepath.Join(tmpDir, ".config", "genesis", "credentials"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "credentials dir should be 0700")
}

func TestRedactKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sk-ant-abcdef1234567890xyz", "sk-a...0xyz"},
		{"short", "****"},
		{"12345678", "****"},
		{"123456789", "1234...6789"},
		{"", "****"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, RedactKey(tt.input), "RedactKey(%q)", tt.input)
	}
}

func TestLoadCredentialMissing(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := loadFromFile("nonexistent-provider")
	require.Error(t, err)
}
