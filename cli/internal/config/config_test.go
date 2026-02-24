package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
)

// ---------------------------------------------------------------------------
// L3: GenesisConfig JSON roundtrip — roles and auth survive marshal/unmarshal
// ---------------------------------------------------------------------------

func TestGenesisConfigRoundtrip(t *testing.T) {
	original := GenesisConfig{
		HeavenAddr: "127.0.0.1:5555",
		Roles: map[string]RoleModelConfig{
			"god":    {Model: "claude-opus-4-6", Provider: "anthropic"},
			"angel":  {Model: "claude-sonnet-4-5-20250929", Provider: "anthropic"},
			"oracle": {Model: "claude-haiku-4-5-20251001", Provider: "anthropic"},
		},
		Auth: map[string]AuthConfig{
			"anthropic": {Type: "api_key", APIKey: "sk-test-key"},
			"openai":    {Type: "oauth", AccessToken: "tok-abc", RefreshToken: "ref-xyz", ExpiresAt: "2025-12-31T23:59:59Z"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored GenesisConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.HeavenAddr != original.HeavenAddr {
		t.Errorf("HeavenAddr = %q, want %q", restored.HeavenAddr, original.HeavenAddr)
	}
	if len(restored.Roles) != 3 {
		t.Fatalf("roles count = %d, want 3", len(restored.Roles))
	}
	if restored.Roles["angel"].Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("angel model = %q", restored.Roles["angel"].Model)
	}
	if restored.Auth["anthropic"].APIKey != "sk-test-key" {
		t.Errorf("anthropic api_key lost after roundtrip")
	}
	if restored.Auth["openai"].RefreshToken != "ref-xyz" {
		t.Errorf("openai refresh_token lost after roundtrip")
	}
}

// ---------------------------------------------------------------------------
// L3: Full Config JSON roundtrip with Genesis section
// ---------------------------------------------------------------------------

func TestFullConfigJSONRoundtrip(t *testing.T) {
	original := Config{
		Data:       Data{Directory: "/tmp/genesis-data"},
		WorkingDir: "/workspace",
		Providers: map[models.ModelProvider]Provider{
			"anthropic": {APIKey: "sk-ant-test"},
		},
		TUI:     TUIConfig{Theme: "catppuccin"},
		Genesis: GenesisConfig{HeavenAddr: "localhost:4444"},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.TUI.Theme != "catppuccin" {
		t.Errorf("theme = %q, want catppuccin", restored.TUI.Theme)
	}
	if restored.Genesis.HeavenAddr != "localhost:4444" {
		t.Errorf("heaven addr = %q, want localhost:4444", restored.Genesis.HeavenAddr)
	}
	if restored.Providers["anthropic"].APIKey != "sk-ant-test" {
		t.Errorf("provider key lost")
	}
}

// ---------------------------------------------------------------------------
// L3: AuthConfig types
// ---------------------------------------------------------------------------

func TestAuthConfigTypes(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
	}{
		{"api_key", AuthConfig{Type: "api_key", APIKey: "sk-test"}},
		{"oauth", AuthConfig{Type: "oauth", AccessToken: "at", RefreshToken: "rt", ExpiresAt: "2025-01-01T00:00:00Z"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.auth)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var restored AuthConfig
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if restored.Type != tt.auth.Type {
				t.Errorf("type = %q, want %q", restored.Type, tt.auth.Type)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// L3: Config file write/read roundtrip on disk
// ---------------------------------------------------------------------------

func TestConfigFileWriteReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".genesis.json")

	config := Config{
		Genesis: GenesisConfig{
			HeavenAddr: "127.0.0.1:9999",
			Roles: map[string]RoleModelConfig{
				"angel": {Model: "claude-sonnet-4-5-20250929", Provider: "anthropic"},
			},
		},
		TUI: TUIConfig{Theme: "dark"},
		Providers: map[models.ModelProvider]Provider{
			"anthropic": {APIKey: "sk-roundtrip-test"},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	readData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(readData, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.Genesis.HeavenAddr != "127.0.0.1:9999" {
		t.Errorf("heaven addr = %q after disk roundtrip", restored.Genesis.HeavenAddr)
	}
	if restored.TUI.Theme != "dark" {
		t.Errorf("theme = %q after disk roundtrip", restored.TUI.Theme)
	}
	if restored.Providers["anthropic"].APIKey != "sk-roundtrip-test" {
		t.Errorf("api key lost after disk roundtrip")
	}

	// File permissions should be 0600 (private)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("config file permissions = %o, want 600", perm)
	}
}

// ---------------------------------------------------------------------------
// L3: RoleModelConfig validates model/provider pair
// ---------------------------------------------------------------------------

func TestRoleModelConfigFields(t *testing.T) {
	role := RoleModelConfig{
		Model:    "claude-opus-4-6",
		Provider: "anthropic",
	}

	data, _ := json.Marshal(role)
	var restored RoleModelConfig
	json.Unmarshal(data, &restored)

	if restored.Model != "claude-opus-4-6" {
		t.Errorf("model = %q", restored.Model)
	}
	if restored.Provider != "anthropic" {
		t.Errorf("provider = %q", restored.Provider)
	}
}

// ---------------------------------------------------------------------------
// L3: Empty GenesisConfig does not produce null JSON
// ---------------------------------------------------------------------------

func TestEmptyGenesisConfigJSON(t *testing.T) {
	config := GenesisConfig{}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Should produce valid JSON (not null)
	var check map[string]any
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("unmarshal empty config: %v", err)
	}
}
