package dialog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/llm/models"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/styles"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
)

// RoleModelSelection holds the selected model for a Genesis role.
type RoleModelSelection struct {
	Role     string
	Model    models.Model
	Provider models.ModelProvider
}

// RoleModelSelectedMsg is sent when a role-model assignment is confirmed.
type RoleModelSelectedMsg struct {
	Role  string
	Model models.Model
}

// CloseRoleModelDialogMsg is sent when the role model dialog is closed.
type CloseRoleModelDialogMsg struct{}

// OpenRoleModelDialogMsg is sent to open the role model picker.
type OpenRoleModelDialogMsg struct{}

// RoleModelDialog is the interface for the per-role model picker.
type RoleModelDialog interface {
	tea.Model
	layout.Bindings
}

type roleModelDialogCmp struct {
	roles           []string
	roleIdx         int
	models          []models.Model
	modelIdx        int
	scrollOffset    int
	width           int
	height          int
}

const (
	roleModelMaxVisible = 10
	roleModelMaxWidth   = 50
)

type roleModelKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	Tab    key.Binding
}

var roleModelKeys = roleModelKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "previous model"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "next model"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "previous role"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "next role"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "assign model to role"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next role"),
	),
}

func (r *roleModelDialogCmp) Init() tea.Cmd {
	r.setupModels()
	return nil
}

func (r *roleModelDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, roleModelKeys.Up):
			if r.modelIdx > 0 {
				r.modelIdx--
			} else {
				r.modelIdx = len(r.models) - 1
				r.scrollOffset = max(0, len(r.models)-roleModelMaxVisible)
			}
			if r.modelIdx < r.scrollOffset {
				r.scrollOffset = r.modelIdx
			}
		case key.Matches(msg, roleModelKeys.Down):
			if r.modelIdx < len(r.models)-1 {
				r.modelIdx++
			} else {
				r.modelIdx = 0
				r.scrollOffset = 0
			}
			if r.modelIdx >= r.scrollOffset+roleModelMaxVisible {
				r.scrollOffset = r.modelIdx - (roleModelMaxVisible - 1)
			}
		case key.Matches(msg, roleModelKeys.Left):
			r.switchRole(-1)
		case key.Matches(msg, roleModelKeys.Right), key.Matches(msg, roleModelKeys.Tab):
			r.switchRole(1)
		case key.Matches(msg, roleModelKeys.Enter):
			if len(r.models) > 0 && r.modelIdx < len(r.models) {
				return r, util.CmdHandler(RoleModelSelectedMsg{
					Role:  r.roles[r.roleIdx],
					Model: r.models[r.modelIdx],
				})
			}
		case key.Matches(msg, roleModelKeys.Escape):
			return r, util.CmdHandler(CloseRoleModelDialogMsg{})
		}
	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height
	}
	return r, nil
}

func (r *roleModelDialogCmp) switchRole(offset int) {
	r.roleIdx += offset
	if r.roleIdx < 0 {
		r.roleIdx = len(r.roles) - 1
	}
	if r.roleIdx >= len(r.roles) {
		r.roleIdx = 0
	}
	r.setupModels()
}

func (r *roleModelDialogCmp) setupModels() {
	// Get all available models across all providers
	var allModels []models.Model
	for _, m := range models.SupportedModels {
		allModels = append(allModels, m)
	}
	slices.SortFunc(allModels, func(a, b models.Model) int {
		if a.Name > b.Name {
			return -1
		}
		if a.Name < b.Name {
			return 1
		}
		return 0
	})
	r.models = allModels
	r.modelIdx = 0
	r.scrollOffset = 0
}

func (r *roleModelDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Role tabs
	var tabs []string
	for i, role := range r.roles {
		tabStyle := baseStyle.Padding(0, 2)
		displayName := strings.ToUpper(role[:1]) + role[1:]
		if i == r.roleIdx {
			tabStyle = tabStyle.
				Background(t.Primary()).
				Foreground(t.Background()).
				Bold(true)
		} else {
			tabStyle = tabStyle.
				Foreground(t.TextMuted())
		}
		tabs = append(tabs, tabStyle.Render(displayName))
	}
	tabRow := lipgloss.JoinHorizontal(lipgloss.Left, tabs...)

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(roleModelMaxWidth).
		Render("𖤍 Assign Model to Role")

	// Model list
	endIdx := min(r.scrollOffset+roleModelMaxVisible, len(r.models))
	modelItems := make([]string, 0, endIdx-r.scrollOffset)
	for i := r.scrollOffset; i < endIdx; i++ {
		itemStyle := baseStyle.Width(roleModelMaxWidth)
		if i == r.modelIdx {
			itemStyle = itemStyle.Background(t.Primary()).
				Foreground(t.Background()).Bold(true)
		}
		label := fmt.Sprintf("%s (%s)", r.models[i].Name, r.models[i].Provider)
		modelItems = append(modelItems, itemStyle.Render(label))
	}

	var scrollHint string
	if len(r.models) > roleModelMaxVisible {
		if r.scrollOffset > 0 {
			scrollHint += "↑ "
		}
		if r.scrollOffset+roleModelMaxVisible < len(r.models) {
			scrollHint += "↓"
		}
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(roleModelMaxWidth).Render(""),
		tabRow,
		baseStyle.Width(roleModelMaxWidth).Render(""),
		lipgloss.JoinVertical(lipgloss.Left, modelItems...),
	)
	if scrollHint != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, content,
			baseStyle.Foreground(t.Primary()).Width(roleModelMaxWidth).Align(lipgloss.Right).Render(scrollHint))
	}

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (r *roleModelDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(roleModelKeys)
}

// NewRoleModelDialogCmp creates a new per-role model picker dialog.
func NewRoleModelDialogCmp() RoleModelDialog {
	return &roleModelDialogCmp{
		roles: []string{"god", "angel", "oracle"},
	}
}
