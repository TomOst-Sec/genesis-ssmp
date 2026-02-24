package genesis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/genesis-ssmp/genesis/cli/internal/config"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/god"
)

// MissionRunner orchestrates a full solo mission execution:
// plan → pack → send (to Angel/Opus) → integrate → report.
type MissionRunner struct {
	repoPath string
}

// NewMissionRunner creates a runner for the given repository.
func NewMissionRunner(repoPath string) *MissionRunner {
	return &MissionRunner{repoPath: repoPath}
}

// Run executes a full solo mission pipeline.
func (mr *MissionRunner) Run(taskDesc string) (*god.SoloResult, error) {
	cfg := config.Get()

	// 1. Create Heaven client
	heavenAddr := cfg.Genesis.HeavenAddr
	if heavenAddr == "" {
		heavenAddr = "127.0.0.1:4444"
	}
	heaven := god.NewHeavenClient("http://" + heavenAddr)

	// 2. Create Angel provider (routes through Claude Code CLI for OAuth)
	angelRole, ok := cfg.Genesis.Roles["angel"]
	if !ok {
		return nil, fmt.Errorf("no 'angel' role configured in genesis.roles")
	}

	// Look up the model to get the API model ID
	apiModel := string(angelRole.Model)
	if model, exists := models.SupportedModels[angelRole.Model]; exists {
		apiModel = model.APIModel
	}

	provider := NewAnthropicAngelProvider(apiModel, mr.repoPath)

	// 3. Create SoloExecutor
	soloConfig := god.SoloConfig{
		TokenBudget:  32000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: true,
	}
	executor := god.NewSoloExecutor(heaven, provider, soloConfig)

	// 4. Execute
	result, err := executor.Execute(taskDesc, mr.repoPath)
	if err != nil {
		return nil, fmt.Errorf("mission execute: %w", err)
	}

	return result, nil
}

// FormatMissionResult formats a SoloResult into a human-readable summary.
func FormatMissionResult(result *god.SoloResult) string {
	var sb strings.Builder

	if result.Success {
		sb.WriteString("Mission completed successfully!\n\n")
	} else {
		sb.WriteString("Mission failed.\n")
		if result.Error != "" {
			fmt.Fprintf(&sb, "Error: %s\n\n", result.Error)
		}
	}

	sb.WriteString("METRICS:\n")
	fmt.Fprintf(&sb, "  tokens_in:       %d\n", result.TokensIn)
	fmt.Fprintf(&sb, "  tokens_out:      %d\n", result.TokensOut)
	fmt.Fprintf(&sb, "  cache_read:      %d\n", result.CacheReadTokens)
	fmt.Fprintf(&sb, "  cache_creation:  %d\n", result.CacheCreationTokens)
	fmt.Fprintf(&sb, "  cost_usd:        $%.6f\n", result.CostUSD)
	fmt.Fprintf(&sb, "  provider_calls:  %d\n", result.Calls)
	fmt.Fprintf(&sb, "  pf_calls:        %d\n", result.PFCalls)
	fmt.Fprintf(&sb, "  turns:           %d\n", result.Turns)
	if result.CLIDurationMS > 0 {
		fmt.Fprintf(&sb, "  cli_duration:    %dms\n", result.CLIDurationMS)
	}

	if len(result.FilesModified) > 0 {
		fmt.Fprintf(&sb, "  files_modified:  %v\n", result.FilesModified)
	}
	if len(result.FilesCreated) > 0 {
		fmt.Fprintf(&sb, "  files_created:   %v\n", result.FilesCreated)
	}

	return sb.String()
}

// ReadMissionFile reads a MISSION.md file from the given directory.
func ReadMissionFile(dir string) (string, error) {
	candidates := []string{"MISSION.md", "TEST_PROMPT.md"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
	}
	return "", fmt.Errorf("no MISSION.md or TEST_PROMPT.md found in %s", dir)
}
