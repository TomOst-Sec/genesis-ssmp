package dialog

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/brand"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/util"
)

// ShowWelcomeDialogMsg signals the welcome dialog should be shown.
type ShowWelcomeDialogMsg struct{ Show bool }

// CloseWelcomeDialogMsg signals the welcome dialog should be closed.
type CloseWelcomeDialogMsg struct{}

// WelcomeDialog is the first-run welcome dialog interface.
type WelcomeDialog interface {
	tea.Model
	layout.Bindings
}

type welcomeDialogCmp struct {
	width  int
	height int
}

type welcomeKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

var welcomeKeys = welcomeKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter", "continue"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

func (w *welcomeDialogCmp) Init() tea.Cmd {
	return nil
}

func (w *welcomeDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, welcomeKeys.Enter), key.Matches(msg, welcomeKeys.Escape):
			return w, util.CmdHandler(CloseWelcomeDialogMsg{})
		}
	case tea.WindowSizeMsg:
		w.width = msg.Width
		w.height = msg.Height
	}
	return w, nil
}

func (w *welcomeDialogCmp) View() string {
	t := theme.CurrentTheme()
	if t == nil {
		return ""
	}

	logo := brand.RenderLogo()

	shortcuts := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Render(`
  Key Shortcuts:
    ctrl+k    Command palette
    ctrl+s    Switch session
    ctrl+n    New session
    ctrl+o    Select model
    ctrl+t    Switch theme
    ctrl+b    File tree sidebar
    ctrl+f    File picker
    ctrl+?    Help
    ctrl+c    Quit

  Type / to see all commands.
  Press Enter to continue.`)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		logo,
		"",
		shortcuts,
	)

	return lipgloss.NewStyle().
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		BorderBackground(t.Background()).
		Render(content)
}

func (w *welcomeDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(welcomeKeys)
}

// NewWelcomeDialogCmp creates a new welcome dialog.
func NewWelcomeDialogCmp() WelcomeDialog {
	return &welcomeDialogCmp{}
}
