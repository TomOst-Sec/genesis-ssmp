package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/genesis-ssmp/genesis/cli/internal/auth"
	"github.com/genesis-ssmp/genesis/cli/internal/config"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an LLM provider",
	Long: `Authenticate with an LLM provider outside the TUI.

Interactive mode (default):
  genesis login

Non-interactive:
  genesis login --provider anthropic --api-key "sk-ant-..."
  genesis login --status`,
	RunE: runLogin,
}

func init() {
	loginCmd.Flags().String("provider", "", "Provider name (anthropic, openai, gemini, openrouter, groq, xai, copilot)")
	loginCmd.Flags().String("api-key", "", "API key to set directly (requires --provider)")
	loginCmd.Flags().Bool("status", false, "Show current auth status for all providers")
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	_, err := config.Load(".", false)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	statusFlag, _ := cmd.Flags().GetBool("status")
	providerFlag, _ := cmd.Flags().GetString("provider")
	apiKeyFlag, _ := cmd.Flags().GetString("api-key")

	if statusFlag {
		return loginStatus()
	}
	if providerFlag != "" && apiKeyFlag != "" {
		return loginDirect(providerFlag, apiKeyFlag)
	}
	if providerFlag != "" && apiKeyFlag == "" {
		return fmt.Errorf("--provider requires --api-key (or omit both for interactive mode)")
	}
	if providerFlag == "" && apiKeyFlag != "" {
		return fmt.Errorf("--api-key requires --provider")
	}

	return loginInteractive()
}

func loginStatus() error {
	cfg := config.Get()
	fmt.Println("Genesis Auth Status")
	fmt.Println("========================================")

	for _, cap := range auth.ProviderCapabilities {
		prov, hasKey := cfg.Providers[cap.ProviderKey]
		status := "NOT CONFIGURED"
		detail := ""
		if hasKey && prov.APIKey != "" {
			status = "OK"
			detail = fmt.Sprintf("  key=%s", auth.RedactKey(prov.APIKey))
		}

		source := auth.CredentialSource(string(cap.ProviderKey))
		if source != "" {
			detail += fmt.Sprintf("  storage=%s", source)
		}

		fmt.Printf("  %-18s %-16s%s\n", cap.Name, status, detail)
	}

	fmt.Println("========================================")
	return nil
}

func loginDirect(providerName, apiKey string) error {
	providerKey := models.ModelProvider(strings.ToLower(providerName))
	cap := auth.FindCapability(providerKey)
	if cap == nil {
		return fmt.Errorf("unknown provider: %s\nAvailable: %s", providerName, availableProviderNames())
	}

	// OAuth tokens (sk-ant-o*) are for consumer Claude API, not api.anthropic.com
	if strings.HasPrefix(apiKey, "sk-ant-o") {
		fmt.Printf("OAuth token detected — skipping API validation.\n")
		return persistCredential(*cap, apiKey)
	}

	apiKeyCfg, hasAPIKey := cap.Methods[auth.MethodAPIKey]
	if hasAPIKey && apiKeyCfg.ValidationURL != "" {
		fmt.Printf("Validating %s API key...", cap.Name)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result := auth.ValidateAPIKey(ctx, apiKeyCfg, apiKey)
		if result.Error != nil {
			fmt.Println(" FAILED")
			return fmt.Errorf("validation failed: %w", result.Error)
		}
		if !result.Valid {
			fmt.Println(" INVALID")
			return fmt.Errorf("invalid API key for %s", cap.Name)
		}
		fmt.Printf(" OK\n")
	}

	return persistCredential(*cap, apiKey)
}

func availableProviderNames() string {
	var names []string
	for _, cap := range auth.ProviderCapabilities {
		names = append(names, string(cap.ProviderKey))
	}
	return strings.Join(names, ", ")
}

// -- interactive flow --

type cliMethodEntry struct {
	method auth.AuthMethod
	config auth.MethodConfig
}

func enabledMethodEntries(cap auth.ProviderCapability) []cliMethodEntry {
	var result []cliMethodEntry
	for _, m := range auth.EnabledMethods(cap) {
		if m.Config.Enabled {
			result = append(result, cliMethodEntry{method: m.Method, config: m.Config})
		}
	}
	return result
}

func methodLabel(m cliMethodEntry) string {
	label := auth.MethodDisplayName(m.method)
	if m.method == auth.MethodBridgeImport && m.config.Bridge != nil && m.config.Bridge.Label != "" {
		label = m.config.Bridge.Label
	}
	if m.config.Bridge != nil && m.config.Bridge.SupportLevel != "" {
		label += fmt.Sprintf(" [%s]", m.config.Bridge.SupportLevel)
	}
	return label
}

func loginInteractive() error {
	scanner := bufio.NewScanner(os.Stdin)

	providers := auth.InteractiveProviders()
	fmt.Println("Select a provider:")
	fmt.Println()
	for i, p := range providers {
		fmt.Printf("  [%d] %s\n", i+1, p.Name)
	}
	fmt.Println()

	providerIdx, err := promptChoice(scanner, "Provider", 1, len(providers))
	if err != nil {
		return err
	}
	selectedProvider := providers[providerIdx-1]

	methods := enabledMethodEntries(selectedProvider)
	if len(methods) == 0 {
		return fmt.Errorf("no enabled auth methods for %s", selectedProvider.Name)
	}

	var selectedMethod cliMethodEntry

	if len(methods) == 1 {
		selectedMethod = methods[0]
		fmt.Printf("Using: %s\n", methodLabel(selectedMethod))
	} else {
		fmt.Println()
		fmt.Printf("Auth methods for %s:\n", selectedProvider.Name)
		fmt.Println()
		for i, m := range methods {
			fmt.Printf("  [%d] %s\n", i+1, methodLabel(m))
		}
		fmt.Println()

		methodIdx, err := promptChoice(scanner, "Method", 1, len(methods))
		if err != nil {
			return err
		}
		selectedMethod = methods[methodIdx-1]
	}

	switch selectedMethod.method {
	case auth.MethodAPIKey:
		return flowAPIKey(scanner, selectedProvider, selectedMethod.config)
	case auth.MethodDeviceFlow:
		return flowDeviceFlow(selectedProvider, selectedMethod.config)
	case auth.MethodBridgeImport:
		return flowBridge(scanner, selectedProvider, selectedMethod.config)
	default:
		return fmt.Errorf("unsupported method: %s", selectedMethod.method)
	}
}

// -- auth flows --

func flowAPIKey(scanner *bufio.Scanner, provider auth.ProviderCapability, cfg auth.MethodConfig) error {
	fmt.Println()
	if cfg.KeyPageURL != "" {
		fmt.Printf("Get your API key at: %s\n", cfg.KeyPageURL)
	}
	if cfg.KeyHint != "" {
		fmt.Printf("Hint: %s\n", cfg.KeyHint)
	}
	fmt.Println()

	apiKey, err := promptString(scanner, "Paste API key")
	if err != nil {
		return err
	}
	if apiKey == "" {
		return fmt.Errorf("no key entered")
	}

	if cfg.ValidationURL != "" {
		fmt.Print("Validating...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result := auth.ValidateAPIKey(ctx, cfg, apiKey)
		if result.Error != nil {
			fmt.Println(" FAILED")
			return fmt.Errorf("validation failed: %w", result.Error)
		}
		if !result.Valid {
			fmt.Println(" INVALID")
			return fmt.Errorf("API key rejected by %s", provider.Name)
		}
		fmt.Println(" OK")
	}

	return persistCredential(provider, apiKey)
}

func flowDeviceFlow(provider auth.ProviderCapability, cfg auth.MethodConfig) error {
	if cfg.ClientID == "" {
		return fmt.Errorf("device flow unavailable: no client_id configured")
	}

	fmt.Println()
	fmt.Print("Requesting device code...")

	ctx := context.Background()
	code, err := auth.RequestDeviceCode(ctx, cfg)
	if err != nil {
		return fmt.Errorf("device code request: %w", err)
	}
	fmt.Println(" OK")
	fmt.Println()

	fmt.Println("Open this URL in your browser:")
	fmt.Printf("  %s\n", code.VerificationURI)
	fmt.Println()
	fmt.Println("Enter this code:")
	fmt.Printf("  %s\n", code.UserCode)
	fmt.Println()

	tryOpenBrowser(code.VerificationURI)

	fmt.Println("Waiting for authorization (press Ctrl+C to cancel)...")

	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(code.ExpiresIn)*time.Second)
	defer cancel()

	token, err := auth.PollForToken(pollCtx, cfg, code.DeviceCode, code.Interval)
	if err != nil {
		return fmt.Errorf("device flow: %w", err)
	}

	fmt.Println("Authorization received.")
	return persistCredential(provider, token.AccessToken)
}

func flowBridge(scanner *bufio.Scanner, provider auth.ProviderCapability, cfg auth.MethodConfig) error {
	bridge := cfg.Bridge
	if bridge == nil {
		return fmt.Errorf("no bridge configuration for %s", provider.Name)
	}

	if bridge.SupportLevel == "unsupported" {
		fmt.Println()
		fmt.Println("WARNING: UNSUPPORTED AUTH METHOD")
		fmt.Println("========================================")
		if bridge.SupportNote != "" {
			fmt.Println(bridge.SupportNote)
		}
		fmt.Println("========================================")
		fmt.Println()
		proceed, err := promptYesNo(scanner, "Proceed anyway?")
		if err != nil {
			return err
		}
		if !proceed {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	fmt.Printf("Detecting %s CLI...\n", bridge.CLIName)
	info := auth.DetectBridge(bridge)

	if info.CLIFound {
		fmt.Printf("  CLI found: %s\n", info.CLIPath)
	} else {
		fmt.Println("  CLI not found")
	}
	if info.CacheFound {
		fmt.Printf("  Cache found: %s\n", info.CachePath)
	}
	fmt.Println()

	if info.CacheFound || len(bridge.TokenArgs) > 0 {
		fmt.Print("Importing existing session...")
		result, err := auth.ImportBridgeToken(context.Background(), bridge)
		if err == nil && result != nil {
			fmt.Println(" OK")
			fmt.Printf("  Source: %s\n", result.SourcePath)
			fmt.Printf("  Key:    %s\n", auth.RedactKey(result.Token))
			return validateAndPersistBridgeToken(provider, result.Token)
		}
		fmt.Printf(" not found (%v)\n", err)
	}

	if info.CLIFound {
		fmt.Println()
		fmt.Printf("No existing session. Login via %s?\n", bridge.CLIName)
		proceed, err := promptYesNo(scanner, "Launch login")
		if err != nil {
			return err
		}
		if proceed {
			return bridgeLoginAndImport(provider, bridge)
		}
	}

	fmt.Printf("Install %s CLI or use API Key instead.\n", bridge.CLIName)
	return fmt.Errorf("bridge authentication not available")
}

func validateAndPersistBridgeToken(provider auth.ProviderCapability, token string) error {
	apiKeyCfg, hasAPIKey := provider.Methods[auth.MethodAPIKey]
	if hasAPIKey && apiKeyCfg.ValidationURL != "" {
		fmt.Print("Validating token...")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// OAuth tokens (sk-ant-o*) are for the consumer API, not api.anthropic.com
		if strings.HasPrefix(token, "sk-ant-o") {
			fmt.Println(" skipped (OAuth token)")
			return persistCredential(provider, token)
		}

		result := auth.ValidateAPIKey(ctx, apiKeyCfg, token)
		if result.Error != nil || !result.Valid {
			fmt.Println(" FAILED")
			errMsg := "token rejected"
			if result.Error != nil {
				errMsg = result.Error.Error()
			}
			return fmt.Errorf("validation failed: %s", errMsg)
		}
		fmt.Println(" OK")
	}

	return persistCredential(provider, token)
}

func bridgeLoginAndImport(provider auth.ProviderCapability, bridge *auth.BridgeConfig) error {
	loginCmd, err := auth.BridgeLoginCmd(bridge)
	if err != nil {
		return fmt.Errorf("cannot build login command: %w", err)
	}

	loginCmd.Stdin = os.Stdin
	loginCmd.Stdout = os.Stdout
	loginCmd.Stderr = os.Stderr

	fmt.Printf("Running: %s %s\n", bridge.CLIName, strings.Join(bridge.LoginArgs, " "))
	fmt.Println("========================================")

	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("CLI login failed: %w", err)
	}

	fmt.Println("========================================")
	fmt.Println()

	fmt.Print("Importing credentials...")
	result, err := auth.ImportBridgeToken(context.Background(), bridge)
	if err != nil {
		return fmt.Errorf("import after login failed: %w", err)
	}
	fmt.Println(" OK")
	fmt.Printf("  Source: %s\n", result.SourcePath)
	fmt.Printf("  Key:    %s\n", auth.RedactKey(result.Token))

	return validateAndPersistBridgeToken(provider, result.Token)
}

// -- persistence --

func persistCredential(provider auth.ProviderCapability, credential string) error {
	providerKey := string(provider.ProviderKey)

	if err := config.SaveProviderAPIKey(providerKey, credential); err != nil {
		return fmt.Errorf("save to config: %w", err)
	}

	if err := auth.StoreCredential(providerKey, credential); err != nil {
		fmt.Printf("Note: keyring storage failed (%v). Key saved to config file.\n", err)
	}

	fmt.Printf("\nConnected to %s.\n", provider.Name)
	return nil
}

// -- prompt helpers --

func promptChoice(scanner *bufio.Scanner, label string, min, max int) (int, error) {
	for {
		fmt.Printf("%s [%d-%d]: ", label, min, max)
		if !scanner.Scan() {
			return 0, fmt.Errorf("input cancelled")
		}
		text := strings.TrimSpace(scanner.Text())
		n, err := strconv.Atoi(text)
		if err != nil || n < min || n > max {
			fmt.Printf("Please enter a number between %d and %d.\n", min, max)
			continue
		}
		return n, nil
	}
}

func promptString(scanner *bufio.Scanner, label string) (string, error) {
	fmt.Printf("%s: ", label)
	if !scanner.Scan() {
		return "", fmt.Errorf("input cancelled")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func promptYesNo(scanner *bufio.Scanner, question string) (bool, error) {
	fmt.Printf("%s [y/N]: ", question)
	if !scanner.Scan() {
		return false, fmt.Errorf("input cancelled")
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// -- browser helper --

func tryOpenBrowser(url string) {
	openers := [][]string{
		{"xdg-open", url},
		{"gio", "open", url},
	}
	if browser := os.Getenv("BROWSER"); browser != "" {
		openers = append(openers, []string{browser, url})
	}
	for _, args := range openers {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err == nil {
			return
		}
	}
}
