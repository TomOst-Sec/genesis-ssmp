package auth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	keyringService = "genesis-cli"
	keyringAttrKey = "provider"
)

// StoreCredential stores a credential, trying OS keyring first then file fallback.
func StoreCredential(provider, credential string) error {
	if err := storeInKeyring(provider, credential); err == nil {
		return nil
	}
	return storeInFile(provider, credential)
}

// LoadCredential loads a credential, trying OS keyring first then file fallback.
func LoadCredential(provider string) (string, error) {
	if cred, err := loadFromKeyring(provider); err == nil && cred != "" {
		return cred, nil
	}
	return loadFromFile(provider)
}

// CredentialSource returns where a credential is stored: "keyring", "file", or "".
func CredentialSource(provider string) string {
	if cred, err := loadFromKeyring(provider); err == nil && cred != "" {
		return "keyring"
	}
	if _, err := loadFromFile(provider); err == nil {
		return "file"
	}
	return ""
}

// HasSecretTool checks if secret-tool is available on the system.
func HasSecretTool() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

func storeInKeyring(provider, credential string) error {
	path, err := exec.LookPath("secret-tool")
	if err != nil {
		return fmt.Errorf("secret-tool not found: %w", err)
	}

	cmd := exec.Command(path, "store",
		"--label", fmt.Sprintf("Genesis CLI: %s", provider),
		keyringAttrKey, provider,
		"service", keyringService,
	)
	cmd.Stdin = strings.NewReader(credential)
	return cmd.Run()
}

func loadFromKeyring(provider string) (string, error) {
	path, err := exec.LookPath("secret-tool")
	if err != nil {
		return "", err
	}

	cmd := exec.Command(path, "lookup",
		keyringAttrKey, provider,
		"service", keyringService,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func credentialDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".config", "genesis", "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func credentialFilePath(provider string) (string, error) {
	dir, err := credentialDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".key"), nil
}

func storeInFile(provider, credential string) error {
	path, err := credentialFilePath(provider)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(credential), 0o600)
}

func loadFromFile(provider string) (string, error) {
	path, err := credentialFilePath(provider)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// RedactKey returns a redacted version: first 4 + last 4 chars.
func RedactKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
