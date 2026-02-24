package status

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
)

// ProgressUpdateMsg updates the progress bar.
type ProgressUpdateMsg struct {
	Percent float64
	Label   string
}

// ProgressDoneMsg signals the progress is complete.
type ProgressDoneMsg struct{}

// ProgressCmp is a determinate progress bar component.
type ProgressCmp interface {
	tea.Model
	SetWidth(width int)
}

type progressCmp struct {
	percent float64
	label   string
	width   int
	visible bool
}

func (p *progressCmp) Init() tea.Cmd {
	return nil
}

func (p *progressCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ProgressUpdateMsg:
		p.percent = msg.Percent
		p.label = msg.Label
		p.visible = true
	case ProgressDoneMsg:
		p.visible = false
		p.percent = 0
	}
	return p, nil
}

func (p *progressCmp) View() string {
	if !p.visible {
		return ""
	}

	t := theme.CurrentTheme()
	if t == nil {
		return ""
	}

	barWidth := p.width - 20
	if barWidth < 10 {
		barWidth = 10
	}

	filled := int(float64(barWidth) * p.percent)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	filledStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Render(strings.Repeat("█", filled))

	emptyStyle := lipgloss.NewStyle().
		Foreground(t.BackgroundSecondary()).
		Render(strings.Repeat("░", empty))

	percentStr := fmt.Sprintf("%3d%%", int(p.percent*100))
	percentStyle := lipgloss.NewStyle().
		Foreground(t.TextMuted())

	bar := fmt.Sprintf("[%s%s] %s", filledStyle, emptyStyle, percentStyle.Render(percentStr))

	if p.label != "" {
		labelStyle := lipgloss.NewStyle().
			Foreground(t.TextMuted()).
			Italic(true)
		bar += " " + labelStyle.Render(p.label)
	}

	return bar
}

func (p *progressCmp) SetWidth(width int) {
	p.width = width
}

// NewProgressCmp creates a new progress bar component.
func NewProgressCmp() ProgressCmp {
	return &progressCmp{
		width: 60,
	}
}
