package auth

import "github.com/genesis-ssmp/genesis/cli/internal/llm/models"

// AuthMethod identifies a supported authentication mechanism.
type AuthMethod string

const (
	MethodAPIKey       AuthMethod = "api_key"
	MethodBridgeImport AuthMethod = "bridge_import"
	MethodDeviceFlow   AuthMethod = "device_flow"
	MethodOAuthPKCE    AuthMethod = "oauth_pkce"
)

// MethodConfig holds provider-specific configuration for an auth method.
type MethodConfig struct {
	Enabled        bool
	DisabledReason string // shown in UI when !Enabled

	// API key fields
	KeyPageURL string
	KeyPrefix  string
	KeyHint    string

	// Validation (cheap HTTP call to verify key works)
	ValidationURL    string // endpoint to hit
	ValidationMethod string // "GET" or "POST"; default "GET"
	AuthHeaderKey    string // custom header name (e.g. "x-api-key"); default "Authorization: Bearer"

	// Device flow fields (RFC 8628)
	DeviceAuthURL string
	TokenURL      string
	ClientID      string // empty = device flow disabled at runtime
	Scopes        string

	// Bridge fields (for MethodBridgeImport)
	Bridge *BridgeConfig
}

// ProviderCapability defines what auth methods a provider supports.
type ProviderCapability struct {
	Name        string
	ProviderKey models.ModelProvider
	Methods     map[AuthMethod]MethodConfig
}

// ProviderCapabilities is the global registry of provider auth capabilities.
var ProviderCapabilities = []ProviderCapability{
	{
		Name:        "Anthropic",
		ProviderKey: models.ProviderAnthropic,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:          true,
				KeyPageURL:       "https://console.anthropic.com/settings/keys",
				KeyPrefix:        "sk-ant-",
				KeyHint:          "Starts with sk-ant-. Reliable, works everywhere.",
				ValidationURL:    "https://api.anthropic.com/v1/messages",
				ValidationMethod: "POST",
				AuthHeaderKey:    "x-api-key",
			},
			MethodBridgeImport: {
				Enabled: true,
				Bridge: &BridgeConfig{
					CLIName:     "claude",
					CLIAltNames: []string{"claude-code"},
					LoginArgs:   []string{"login"},
					CachePaths: []string{
						"~/.claude/.credentials.json",
						"~/.claude.json",
						"$XDG_CONFIG_HOME/claude/credentials.json",
						"~/.config/claude/credentials.json",
						"~/.config/claude/auth.json",
						"~/.anthropic/auth.json",
					},
					CacheParser:  "claude_json",
					SupportLevel: "unsupported",
					SupportNote:  "UNSUPPORTED: Imports Claude CLI session token.\nMay break without notice. Not endorsed by Anthropic.\nUse API key (sk-ant-) for reliable access.",
					Label:        "Claude Code [UNSUPPORTED]",
				},
			},
			MethodOAuthPKCE: {
				Enabled:        false,
				DisabledReason: "Not available in this build",
			},
		},
	},
	{
		Name:        "OpenAI",
		ProviderKey: models.ProviderOpenAI,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:       true,
				KeyPageURL:    "https://platform.openai.com/api-keys",
				KeyPrefix:     "sk-",
				KeyHint:       "Starts with sk-. Reliable, works everywhere.",
				ValidationURL: "https://api.openai.com/v1/models",
			},
			MethodBridgeImport: {
				Enabled: true,
				Bridge: &BridgeConfig{
					CLIName:      "codex",
					LoginArgs:    []string{"login"},
					CachePaths:   []string{"~/.codex/auth.json"},
					CacheParser:  "codex_json",
					SupportLevel: "unsupported",
					SupportNote:  "UNSUPPORTED: Imports Codex CLI session token.\nMay break without notice. Not endorsed by OpenAI.\nUse API key (sk-) for reliable access.",
					Label:        "Codex CLI [UNSUPPORTED]",
				},
			},
			MethodOAuthPKCE: {
				Enabled:        false,
				DisabledReason: "Requires registered OAuth client",
			},
		},
	},
	{
		Name:        "Gemini",
		ProviderKey: models.ProviderGemini,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:       true,
				KeyPageURL:    "https://aistudio.google.com/apikey",
				KeyHint:       "Google AI Studio API key",
				ValidationURL: "https://generativelanguage.googleapis.com/v1beta/models",
				AuthHeaderKey: "query:key",
			},
		},
	},
	{
		Name:        "OpenRouter",
		ProviderKey: models.ProviderOpenRouter,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:       true,
				KeyPageURL:    "https://openrouter.ai/keys",
				KeyPrefix:     "sk-or-",
				KeyHint:       "Starts with sk-or-",
				ValidationURL: "https://openrouter.ai/api/v1/models",
			},
		},
	},
	{
		Name:        "Groq",
		ProviderKey: models.ProviderGROQ,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:       true,
				KeyPageURL:    "https://console.groq.com/keys",
				KeyPrefix:     "gsk_",
				KeyHint:       "Starts with gsk_",
				ValidationURL: "https://api.groq.com/openai/v1/models",
			},
		},
	},
	{
		Name:        "xAI",
		ProviderKey: models.ProviderXAI,
		Methods: map[AuthMethod]MethodConfig{
			MethodAPIKey: {
				Enabled:       true,
				KeyPageURL:    "https://console.x.ai/team/default/api-keys",
				KeyPrefix:     "xai-",
				KeyHint:       "Starts with xai-",
				ValidationURL: "https://api.x.ai/v1/models",
			},
		},
	},
	{
		Name:        "GitHub Copilot",
		ProviderKey: models.ProviderCopilot,
		Methods: map[AuthMethod]MethodConfig{
			MethodBridgeImport: {
				Enabled: true,
				Bridge: &BridgeConfig{
					CLIName:      "gh",
					LoginArgs:    []string{"auth", "login", "--web"},
					TokenArgs:    []string{"auth", "token"},
					CachePaths:   []string{"~/.config/github-copilot/hosts.json"},
					CacheParser:  "gh_token",
					SupportLevel: "supported",
					SupportNote:  "Uses GitHub CLI credentials.",
					Label:        "GitHub CLI (gh auth)",
				},
			},
			MethodAPIKey: {
				Enabled:    true,
				KeyPageURL: "https://github.com/settings/tokens",
				KeyPrefix:  "gh",
				KeyHint:    "GitHub token (ghp_ or gho_)",
			},
			MethodDeviceFlow: {
				Enabled:       true,
				DeviceAuthURL: "https://github.com/login/device/code",
				TokenURL:      "https://github.com/login/oauth/access_token",
				ClientID:      "", // Set to registered GitHub OAuth App client_id to enable
				Scopes:        "read:user",
			},
		},
	},
}

// FindCapability returns the ProviderCapability for the given provider key, or nil.
func FindCapability(provider models.ModelProvider) *ProviderCapability {
	for i := range ProviderCapabilities {
		if ProviderCapabilities[i].ProviderKey == provider {
			return &ProviderCapabilities[i]
		}
	}
	return nil
}

// InteractiveProviders returns providers that have at least one enabled method.
func InteractiveProviders() []ProviderCapability {
	var result []ProviderCapability
	for _, cap := range ProviderCapabilities {
		for _, m := range cap.Methods {
			if m.Enabled {
				result = append(result, cap)
				break
			}
		}
	}
	return result
}

// EnabledMethods returns all methods for a provider in stable display order.
// Enabled methods come first; disabled methods are included for display purposes.
func EnabledMethods(cap ProviderCapability) []struct {
	Method AuthMethod
	Config MethodConfig
} {
	var result []struct {
		Method AuthMethod
		Config MethodConfig
	}
	order := []AuthMethod{MethodAPIKey, MethodBridgeImport, MethodDeviceFlow, MethodOAuthPKCE}
	for _, m := range order {
		if cfg, ok := cap.Methods[m]; ok {
			result = append(result, struct {
				Method AuthMethod
				Config MethodConfig
			}{Method: m, Config: cfg})
		}
	}
	return result
}

// MethodDisplayName returns a human-readable name for an auth method.
// If the method has a bridge config with a Label, that is preferred.
func MethodDisplayName(m AuthMethod) string {
	switch m {
	case MethodAPIKey:
		return "API Key"
	case MethodBridgeImport:
		return "Subscription Login (Bridge)"
	case MethodDeviceFlow:
		return "Device Flow (Browser)"
	case MethodOAuthPKCE:
		return "OAuth Login"
	default:
		return string(m)
	}
}

// BridgeDisplayName returns the bridge label if available, otherwise the generic name.
func BridgeDisplayName(cfg MethodConfig) string {
	if cfg.Bridge != nil && cfg.Bridge.Label != "" {
		return cfg.Bridge.Label
	}
	return MethodDisplayName(MethodBridgeImport)
}
