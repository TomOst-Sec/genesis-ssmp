package auth

import (
	"testing"

	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInteractiveProviders(t *testing.T) {
	providers := InteractiveProviders()
	require.GreaterOrEqual(t, len(providers), 7, "should have at least 7 interactive providers")

	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name
	}
	assert.Contains(t, names, "Anthropic")
	assert.Contains(t, names, "OpenAI")
	assert.Contains(t, names, "Gemini")
	assert.Contains(t, names, "GitHub Copilot")
}

func TestFindCapability(t *testing.T) {
	cap := FindCapability(models.ProviderAnthropic)
	require.NotNil(t, cap)
	assert.Equal(t, "Anthropic", cap.Name)
	assert.Equal(t, models.ProviderAnthropic, cap.ProviderKey)

	apiKey, ok := cap.Methods[MethodAPIKey]
	require.True(t, ok, "Anthropic should have api_key method")
	assert.True(t, apiKey.Enabled)
	assert.Contains(t, apiKey.KeyPageURL, "anthropic.com")
}

func TestFindCapabilityMissing(t *testing.T) {
	cap := FindCapability("nonexistent_provider")
	assert.Nil(t, cap)
}

func TestAllAPIKeyProvidersHaveValidationURL(t *testing.T) {
	for _, cap := range ProviderCapabilities {
		if apiKey, ok := cap.Methods[MethodAPIKey]; ok && apiKey.Enabled {
			if cap.ProviderKey == models.ProviderCopilot {
				continue // Copilot uses token exchange, not direct validation
			}
			assert.NotEmpty(t, apiKey.ValidationURL,
				"%s has api_key enabled but no ValidationURL", cap.Name)
		}
	}
}

func TestCopilotHasDeviceFlow(t *testing.T) {
	cap := FindCapability(models.ProviderCopilot)
	require.NotNil(t, cap)

	df, ok := cap.Methods[MethodDeviceFlow]
	require.True(t, ok, "Copilot should have device_flow method")
	assert.True(t, df.Enabled)
	assert.Contains(t, df.DeviceAuthURL, "github.com")
	assert.Contains(t, df.TokenURL, "github.com")
}

func TestEnabledMethods(t *testing.T) {
	cap := FindCapability(models.ProviderOpenAI)
	require.NotNil(t, cap)

	methods := EnabledMethods(*cap)
	require.GreaterOrEqual(t, len(methods), 2, "OpenAI should have at least api_key + bridge")

	// First should be api_key (stable ordering)
	assert.Equal(t, MethodAPIKey, methods[0].Method)
}

func TestMethodDisplayName(t *testing.T) {
	assert.Equal(t, "API Key", MethodDisplayName(MethodAPIKey))
	assert.Equal(t, "Device Flow (Browser)", MethodDisplayName(MethodDeviceFlow))
	assert.Equal(t, "Subscription Login (Bridge)", MethodDisplayName(MethodBridgeImport))
	assert.Equal(t, "OAuth Login", MethodDisplayName(MethodOAuthPKCE))
}

func TestAnthropicOAuthDisabled(t *testing.T) {
	cap := FindCapability(models.ProviderAnthropic)
	require.NotNil(t, cap)

	oauth, ok := cap.Methods[MethodOAuthPKCE]
	require.True(t, ok)
	assert.False(t, oauth.Enabled)
	assert.Contains(t, oauth.DisabledReason, "Not available")
}

func TestBridgeConfigsPresent(t *testing.T) {
	// OpenAI should have an unsupported bridge (Codex CLI)
	openai := FindCapability(models.ProviderOpenAI)
	require.NotNil(t, openai)
	bridge, ok := openai.Methods[MethodBridgeImport]
	require.True(t, ok, "OpenAI should have bridge_import method")
	assert.True(t, bridge.Enabled)
	require.NotNil(t, bridge.Bridge)
	assert.Equal(t, "codex", bridge.Bridge.CLIName)
	assert.Equal(t, "unsupported", bridge.Bridge.SupportLevel)
	assert.Contains(t, bridge.Bridge.Label, "UNSUPPORTED")

	// GitHub Copilot should have a supported bridge
	copilot := FindCapability(models.ProviderCopilot)
	require.NotNil(t, copilot)
	ghBridge, ok := copilot.Methods[MethodBridgeImport]
	require.True(t, ok, "GitHub Copilot should have bridge_import method")
	assert.True(t, ghBridge.Enabled)
	require.NotNil(t, ghBridge.Bridge)
	assert.Equal(t, "gh", ghBridge.Bridge.CLIName)
	assert.Equal(t, "supported", ghBridge.Bridge.SupportLevel)

	// Anthropic should have an unsupported bridge (Claude Code)
	anthropic := FindCapability(models.ProviderAnthropic)
	require.NotNil(t, anthropic)
	claudeBridge, ok := anthropic.Methods[MethodBridgeImport]
	require.True(t, ok, "Anthropic should have bridge_import method (unsupported)")
	assert.True(t, claudeBridge.Enabled)
	require.NotNil(t, claudeBridge.Bridge)
	assert.Equal(t, "unsupported", claudeBridge.Bridge.SupportLevel)
	assert.Equal(t, "claude_json", claudeBridge.Bridge.CacheParser)
	assert.Contains(t, claudeBridge.Bridge.Label, "UNSUPPORTED")
	assert.Contains(t, claudeBridge.Bridge.SupportNote, "UNSUPPORTED")
}

func TestBridgeDisplayName(t *testing.T) {
	openai := FindCapability(models.ProviderOpenAI)
	require.NotNil(t, openai)
	bridge := openai.Methods[MethodBridgeImport]
	assert.Contains(t, BridgeDisplayName(bridge), "UNSUPPORTED")

	// Fallback when no label
	assert.Equal(t, "Subscription Login (Bridge)", BridgeDisplayName(MethodConfig{}))
}

func TestUnsupportedBridgesHaveWarnings(t *testing.T) {
	for _, cap := range ProviderCapabilities {
		bridge, ok := cap.Methods[MethodBridgeImport]
		if !ok || bridge.Bridge == nil {
			continue
		}
		if bridge.Bridge.SupportLevel == "unsupported" {
			assert.Contains(t, bridge.Bridge.SupportNote, "UNSUPPORTED",
				"%s unsupported bridge must mention UNSUPPORTED in SupportNote", cap.Name)
			assert.Contains(t, bridge.Bridge.Label, "UNSUPPORTED",
				"%s unsupported bridge must mention UNSUPPORTED in Label", cap.Name)
			assert.True(t, bridge.Enabled,
				"%s unsupported bridge must be Enabled=true", cap.Name)
		}
	}
}
