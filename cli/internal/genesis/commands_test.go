package genesis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenesisCommandsCount(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	// We expect 14 Genesis-specific commands
	require.Len(t, commands, 14)
}

func TestGenesisCommandIDs(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	expectedIDs := []string{
		"heaven-status",
		"heaven-logs",
		"index-repo",
		"sym-search",
		"sym-def",
		"callers",
		"lease-acquire",
		"validate-manifest",
		"plan-mission",
		"run-mission",
		"swarm-agent-list",
		"connect-provider",
		"select-model",
		"blob-store",
	}

	ids := make([]string, len(commands))
	for i, cmd := range commands {
		ids[i] = cmd.ID
	}

	for _, expected := range expectedIDs {
		assert.Contains(t, ids, expected, "missing command: %s", expected)
	}
}

func TestGenesisCommandIDsNoDuplicates(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	seen := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		if seen[cmd.ID] {
			t.Errorf("duplicate command ID: %s", cmd.ID)
		}
		seen[cmd.ID] = true
	}

	// All 14 expected IDs must be present
	expectedIDs := []string{
		"heaven-status", "heaven-logs", "index-repo", "sym-search",
		"sym-def", "callers", "lease-acquire", "validate-manifest",
		"plan-mission", "run-mission", "swarm-agent-list",
		"connect-provider", "select-model", "blob-store",
	}
	for _, id := range expectedIDs {
		assert.True(t, seen[id], "missing command ID: %s", id)
	}
}

func TestGenesisCommandRolesModel(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	// Each command must have non-empty Title and Description (acts as role context)
	for _, cmd := range commands {
		assert.NotEmpty(t, cmd.Title, "command %s has empty Title", cmd.ID)
		assert.NotEmpty(t, cmd.Description, "command %s has empty Description", cmd.ID)
		// The handler must produce a valid tea.Cmd (not nil)
		assert.NotNil(t, cmd.Handler, "command %s has nil Handler", cmd.ID)
	}

	// Verify specific role-related commands exist and have proper descriptions
	roleCommands := map[string]string{
		"select-model":    "model",
		"connect-provider": "provider",
	}
	for id, keyword := range roleCommands {
		for _, cmd := range commands {
			if cmd.ID == id {
				assert.Contains(t, cmd.Description, keyword,
					"command %s description should mention %q", id, keyword)
			}
		}
	}
}

func TestGenesisCommandHandlersNotNil(t *testing.T) {
	client := NewClient("127.0.0.1:4444")
	commands := GenesisCommands(client)

	for _, cmd := range commands {
		assert.NotNil(t, cmd.Handler, "handler nil for command: %s", cmd.ID)
		assert.NotEmpty(t, cmd.Title, "title empty for command: %s", cmd.ID)
		assert.NotEmpty(t, cmd.Description, "description empty for command: %s", cmd.ID)
	}
}
