package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/genesis-ssmp/genesis/cli/internal/auth"
	"github.com/genesis-ssmp/genesis/cli/internal/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose Genesis CLI configuration",
	Long:  `Run diagnostic checks on your Genesis CLI setup including auth, providers, and system dependencies.`,
}

var doctorAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Diagnose authentication configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config from current directory
		cfg, err := config.Load(".", false)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		fmt.Println("Genesis CLI Auth Diagnostics")
		fmt.Println("========================================")
		fmt.Println()

		// ── Official CLIs ──
		fmt.Println("Official CLIs:")
		cliNames := []struct {
			name string
			alts []string
		}{
			{"codex", nil},
			{"gh", nil},
			{"claude", []string{"claude-code"}},
		}
		for _, cli := range cliNames {
			found := false
			names := append([]string{cli.name}, cli.alts...)
			for _, name := range names {
				if path, err := exec.LookPath(name); err == nil {
					fmt.Printf("  %-14s %s\n", cli.name+":", path)
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("  %-14s not found\n", cli.name+":")
			}
		}
		fmt.Println()

		// ── Providers + Bridge Detection ──
		fmt.Println("Providers:")
		for _, cap := range auth.ProviderCapabilities {
			prov, hasKey := cfg.Providers[cap.ProviderKey]
			status := "NOT CONFIGURED"
			keyDisplay := ""
			if hasKey && prov.APIKey != "" {
				status = "OK"
				keyDisplay = fmt.Sprintf("  key=%s", auth.RedactKey(prov.APIKey))
			}

			fmt.Printf("  %-18s %-16s%s\n", cap.Name, status, keyDisplay)

			// Show methods
			for _, mc := range auth.EnabledMethods(cap) {
				label := auth.MethodDisplayName(mc.Method)
				enabledStr := "enabled"

				if !mc.Config.Enabled {
					enabledStr = fmt.Sprintf("disabled: %s", mc.Config.DisabledReason)
				}

				extra := ""

				// Bridge detection
				if mc.Method == auth.MethodBridgeImport && mc.Config.Bridge != nil {
					label = auth.BridgeDisplayName(mc.Config)
					info := auth.DetectBridge(mc.Config.Bridge)
					parts := []string{}
					if info.CLIFound {
						parts = append(parts, "CLI: yes")
					} else {
						parts = append(parts, "CLI: no")
					}
					if info.CacheFound {
						parts = append(parts, fmt.Sprintf("cache: %s", info.CachePath))
					}
					levelTag := ""
					if mc.Config.Bridge.SupportLevel != "" {
						levelTag = fmt.Sprintf("[%s]", mc.Config.Bridge.SupportLevel)
					}
					extra = fmt.Sprintf("  %s  %s", levelTag, joinStrings(parts))
				}

				// Device flow gating
				if mc.Method == auth.MethodDeviceFlow && mc.Config.ClientID == "" {
					enabledStr = "disabled: no client_id configured"
				}

				fmt.Printf("    %-26s %s%s\n", label, enabledStr, extra)
			}
		}
		fmt.Println()

		// ── Agent Mappings ──
		fmt.Println("Agent Mappings:")
		if cfg.Agents != nil {
			for name, agent := range cfg.Agents {
				fmt.Printf("  %-14s %s\n", name, agent.Model)
			}
		} else {
			fmt.Println("  (no agents configured)")
		}
		fmt.Println()

		// ── Codex CLI ──
		fmt.Println("Codex CLI:")
		cred, err := auth.FindCodexAuth()
		if err != nil {
			fmt.Printf("  ~/.codex/auth.json: NOT FOUND\n")
		} else {
			fmt.Printf("  %s: FOUND (key=%s)\n", cred.SourcePath, auth.RedactKey(cred.APIKey))
		}
		fmt.Println()

		// ── System ──
		fmt.Println("System:")
		for _, tool := range []string{"secret-tool", "xdg-open"} {
			path, err := exec.LookPath(tool)
			if err != nil {
				fmt.Printf("  %-14s not found\n", tool+":")
			} else {
				fmt.Printf("  %-14s %s\n", tool+":", path)
			}
		}

		if home, err := os.UserHomeDir(); err == nil {
			fmt.Printf("  %-14s %s\n", "Config:", filepath.Join(home, ".genesis.json"))
		}

		// Keyring status per provider
		fmt.Println()
		fmt.Println("Credential Storage:")
		for _, cap := range auth.ProviderCapabilities {
			source := auth.CredentialSource(string(cap.ProviderKey))
			if source == "" {
				source = "none"
			}
			p, hasKey := cfg.Providers[cap.ProviderKey]
			cfgStatus := "no"
			if hasKey && p.APIKey != "" {
				cfgStatus = "yes"
			}
			fmt.Printf("  %-18s keyring=%-6s config=%s\n",
				cap.Name, source, cfgStatus)
		}

		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("Run `genesis doctor auth` after adding keys to verify.")

		return nil
	},
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

func init() {
	doctorCmd.AddCommand(doctorAuthCmd)
	rootCmd.AddCommand(doctorCmd)
}
