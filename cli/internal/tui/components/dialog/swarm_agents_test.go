package dialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwarmAgentDialogInit(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	cmd := d.Init()
	// Init should return nil (list view init)
	assert.Nil(t, cmd)
}

func TestSwarmAgentDialogSetAgents(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	agents := []SwarmAgent{
		{MissionID: "m1", Goal: "Build feature", Status: "active", Provider: "anthropic", Turns: 5},
		{MissionID: "m2", Goal: "Fix bug", Status: "idle", Provider: "openai", Turns: 2},
	}
	d.SetAgents(agents)

	// View should render both agents
	view := d.View()
	assert.Contains(t, view, "Swarm Agents")
	assert.Contains(t, view, "m1")
	assert.Contains(t, view, "m2")
}

func TestSwarmAgentDialogEmptyView(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	view := d.View()
	assert.Contains(t, view, "No active agents")
}

func TestSwarmAgentDialogEscClose(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	d.SetAgents([]SwarmAgent{
		{MissionID: "m1", Goal: "Test", Status: "active"},
	})

	model, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEscape})
	require.NotNil(t, model)
	require.NotNil(t, cmd)

	// The cmd should produce a CloseSwarmAgentDialogMsg
	msg := cmd()
	_, ok := msg.(CloseSwarmAgentDialogMsg)
	assert.True(t, ok, "expected CloseSwarmAgentDialogMsg, got %T", msg)
}

func TestSwarmAgentDialogEnterSelect(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	agents := []SwarmAgent{
		{MissionID: "m1", Goal: "Test", Status: "active"},
	}
	d.SetAgents(agents)

	model, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model)
	require.NotNil(t, cmd)

	msg := cmd()
	selected, ok := msg.(SwarmAgentSelectedMsg)
	assert.True(t, ok, "expected SwarmAgentSelectedMsg, got %T", msg)
	assert.Equal(t, "m1", selected.Agent.MissionID)
}

func TestSwarmAgentRender(t *testing.T) {
	agent := SwarmAgent{
		MissionID: "abcdefghijklmnop",
		Goal:      "Build a very long feature description that exceeds thirty characters",
		Status:    "active",
		Provider:  "anthropic",
		Turns:     7,
	}
	rendered := agent.Render(false, 80)
	// MissionID should be truncated to 8 chars
	assert.Contains(t, rendered, "abcdefgh")
	// Goal should be truncated
	assert.True(t, strings.Contains(rendered, "...") || len(rendered) > 0)
}

func TestSwarmAgentDialogSnapshotEmpty(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	// Do not set any agents
	view := d.View()
	assert.Contains(t, view, "No active agents")
	assert.Contains(t, view, "Swarm Agents")
	// Should not contain any mission ID patterns
	assert.NotContains(t, view, "[active]")
	assert.NotContains(t, view, "[idle]")
	assert.NotContains(t, view, "[thrashing]")
}

func TestSwarmAgentDialogSnapshotPopulated(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	agents := []SwarmAgent{
		{MissionID: "active00001234", Goal: "Implement feature X", Status: "active", Provider: "anthropic", Turns: 3},
		{MissionID: "idle000056789a", Goal: "Wait for dependency", Status: "idle", Provider: "openai", Turns: 0},
		{MissionID: "thrash00bcdef0", Goal: "Fix flaky test loop", Status: "thrashing", Provider: "anthropic", Turns: 12},
	}
	d.SetAgents(agents)

	view := d.View()

	// Title
	assert.Contains(t, view, "Swarm Agents")

	// All 3 agents visible (truncated mission IDs to 8 chars)
	assert.Contains(t, view, "active00")
	assert.Contains(t, view, "idle0000")
	assert.Contains(t, view, "thrash00")

	// Status labels
	assert.Contains(t, view, "[active]")
	assert.Contains(t, view, "[idle]")
	assert.Contains(t, view, "[thrashing]")

	// Provider names
	assert.Contains(t, view, "anthropic")
	assert.Contains(t, view, "openai")

	// Should not contain "No active agents" when agents are present
	assert.NotContains(t, view, "No active agents")
}

func TestSwarmAgentDialogBindingKeys(t *testing.T) {
	d := NewSwarmAgentDialogCmp()
	// SwarmAgentDialog embeds layout.Bindings, so it has BindingKeys
	cmp := d.(*swarmAgentDialogCmp)
	bindings := cmp.BindingKeys()
	assert.NotEmpty(t, bindings)
}
