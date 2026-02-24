package diff

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
)

// ViewerCmp is a diff viewer component.
type ViewerCmp interface {
	tea.Model
	SetDiff(diff string)
	SetSize(width, height int)
}

type diffLine struct {
	content  string
	lineType lineType
}

type lineType int

const (
	lineContext lineType = iota
	lineAdded
	lineRemoved
	lineHunkHeader
)

type viewerCmp struct {
	lines        []diffLine
	width        int
	height       int
	scrollOffset int
}

func (v *viewerCmp) Init() tea.Cmd {
	return nil
}

func (v *viewerCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return v, nil
}

func (v *viewerCmp) View() string {
	t := theme.CurrentTheme()
	if t == nil || len(v.lines) == 0 {
		return ""
	}

	visibleHeight := v.height
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	endIdx := v.scrollOffset + visibleHeight
	if endIdx > len(v.lines) {
		endIdx = len(v.lines)
	}

	var rendered []string
	for i := v.scrollOffset; i < endIdx; i++ {
		line := v.lines[i]
		var style lipgloss.Style

		switch line.lineType {
		case lineAdded:
			style = lipgloss.NewStyle().
				Foreground(t.DiffAdded()).
				Background(t.DiffAddedBg()).
				Width(v.width)
		case lineRemoved:
			style = lipgloss.NewStyle().
				Foreground(t.DiffRemoved()).
				Background(t.DiffRemovedBg()).
				Width(v.width)
		case lineHunkHeader:
			style = lipgloss.NewStyle().
				Foreground(t.DiffHunkHeader()).
				Bold(true).
				Width(v.width)
		default:
			style = lipgloss.NewStyle().
				Foreground(t.DiffContext()).
				Background(t.DiffContextBg()).
				Width(v.width)
		}

		rendered = append(rendered, style.Render(line.content))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

func (v *viewerCmp) SetDiff(diff string) {
	v.lines = nil
	for _, line := range strings.Split(diff, "\n") {
		dl := diffLine{content: line}
		switch {
		case strings.HasPrefix(line, "+"):
			dl.lineType = lineAdded
		case strings.HasPrefix(line, "-"):
			dl.lineType = lineRemoved
		case strings.HasPrefix(line, "@@"):
			dl.lineType = lineHunkHeader
		default:
			dl.lineType = lineContext
		}
		v.lines = append(v.lines, dl)
	}
}

func (v *viewerCmp) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// NewViewerCmp creates a new diff viewer.
func NewViewerCmp() ViewerCmp {
	return &viewerCmp{}
}
