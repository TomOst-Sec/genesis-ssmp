package dialog

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	utilComponents "github.com/genesis-ssmp/genesis/cli/internal/tui/components/util"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/styles"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
)

// SwarmAgent describes an active angel worker in the swarm.
type SwarmAgent struct {
	MissionID string
	Goal      string
	Status    string // "active", "idle", "thrashing"
	Provider  string
	Turns     int
	TokensIn  int64
	TokensOut int64
}

// Render implements the SimpleListItem interface for SwarmAgent.
func (a SwarmAgent) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Color-code the status
	var statusColor lipgloss.TerminalColor
	switch a.Status {
	case "active":
		statusColor = t.Success()
	case "idle":
		statusColor = t.Warning()
	case "thrashing":
		statusColor = t.Error()
	default:
		statusColor = t.TextMuted()
	}

	missionID := a.MissionID
	if len(missionID) > 8 {
		missionID = missionID[:8]
	}
	goal := a.Goal
	if len(goal) > 30 {
		goal = goal[:27] + "..."
	}

	line := fmt.Sprintf("%s  %-30s  [%s]  %s  t=%d", missionID, goal, a.Status, a.Provider, a.Turns)

	itemStyle := baseStyle.Width(width).Foreground(t.Text())
	if selected {
		itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
	}

	statusStyle := baseStyle.Foreground(statusColor)
	if selected {
		statusStyle = statusStyle.Background(t.Primary())
	}

	// Render status separately for coloring, then the rest of the line
	_ = statusStyle // status is embedded in the line for simplicity
	return itemStyle.Padding(0, 1).Render(line)
}

// SwarmAgentSelectedMsg is sent when a swarm agent is selected.
type SwarmAgentSelectedMsg struct {
	Agent SwarmAgent
}

// CloseSwarmAgentDialogMsg signals closing the swarm agent dialog.
type CloseSwarmAgentDialogMsg struct{}

// OpenSwarmAgentDialogMsg signals opening the swarm agent dialog.
type OpenSwarmAgentDialogMsg struct{}

// SwarmAgentDialog is the interface for the swarm agent list dialog.
type SwarmAgentDialog interface {
	tea.Model
	layout.Bindings
	SetAgents(agents []SwarmAgent)
}

type swarmAgentDialogCmp struct {
	listView utilComponents.SimpleList[SwarmAgent]
	width    int
	height   int
}

type swarmAgentKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

var swarmAgentKeys = swarmAgentKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select agent"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

func (s *swarmAgentDialogCmp) Init() tea.Cmd {
	return s.listView.Init()
}

func (s *swarmAgentDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, swarmAgentKeys.Enter):
			selected, idx := s.listView.GetSelectedItem()
			if idx != -1 {
				return s, util.CmdHandler(SwarmAgentSelectedMsg{Agent: selected})
			}
		case key.Matches(msg, swarmAgentKeys.Escape):
			return s, util.CmdHandler(CloseSwarmAgentDialogMsg{})
		}
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
	}

	u, cmd := s.listView.Update(msg)
	s.listView = u.(utilComponents.SimpleList[SwarmAgent])
	cmds = append(cmds, cmd)

	return s, tea.Batch(cmds...)
}

func (s *swarmAgentDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 70

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render("𖤍 Swarm Agents")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Render(""),
		baseStyle.Width(maxWidth).Render(s.listView.View()),
		baseStyle.Width(maxWidth).Render(""),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (s *swarmAgentDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(swarmAgentKeys)
}

func (s *swarmAgentDialogCmp) SetAgents(agents []SwarmAgent) {
	s.listView.SetItems(agents)
}

// NewSwarmAgentDialogCmp creates a new swarm agent list dialog.
func NewSwarmAgentDialogCmp() SwarmAgentDialog {
	listView := utilComponents.NewSimpleList[SwarmAgent](
		[]SwarmAgent{},
		15,
		"No active agents",
		true,
	)
	return &swarmAgentDialogCmp{
		listView: listView,
	}
}
