package sidebar

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/layout"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
)

// ToggleSidebarMsg toggles the file tree sidebar.
type ToggleSidebarMsg struct{}

// FileTreeCmp is the sidebar file tree component interface.
type FileTreeCmp interface {
	tea.Model
	layout.Bindings
	SetSize(width, height int)
	SetModifiedFiles(files map[string]bool)
}

type fileEntry struct {
	name     string
	path     string
	isDir    bool
	depth    int
	expanded bool
	children []*fileEntry
}

type fileTreeCmp struct {
	width         int
	height        int
	entries       []*fileEntry
	flatEntries   []*fileEntry
	selectedIdx   int
	scrollOffset  int
	modifiedFiles map[string]bool
	rootPath      string
}

type fileTreeKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	Escape key.Binding
}

var ftKeys = fileTreeKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter", "toggle dir"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

func (f *fileTreeCmp) Init() tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	f.rootPath = cwd
	f.entries = f.scanDir(cwd, 0, 2)
	f.flatEntries = f.flatten(f.entries)
	return nil
}

func (f *fileTreeCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, ftKeys.Up):
			if f.selectedIdx > 0 {
				f.selectedIdx--
			}
			if f.selectedIdx < f.scrollOffset {
				f.scrollOffset = f.selectedIdx
			}
		case key.Matches(msg, ftKeys.Down):
			if f.selectedIdx < len(f.flatEntries)-1 {
				f.selectedIdx++
			}
			visibleHeight := f.height - 2
			if f.selectedIdx >= f.scrollOffset+visibleHeight {
				f.scrollOffset = f.selectedIdx - visibleHeight + 1
			}
		case key.Matches(msg, ftKeys.Toggle):
			if f.selectedIdx < len(f.flatEntries) {
				entry := f.flatEntries[f.selectedIdx]
				if entry.isDir {
					entry.expanded = !entry.expanded
					if entry.expanded && len(entry.children) == 0 {
						entry.children = f.scanDir(entry.path, entry.depth+1, 1)
					}
					f.flatEntries = f.flatten(f.entries)
				}
			}
		}
	}
	return f, nil
}

func (f *fileTreeCmp) View() string {
	t := theme.CurrentTheme()
	if t == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1)

	title := titleStyle.Render("Files")

	visibleHeight := f.height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	var lines []string
	endIdx := f.scrollOffset + visibleHeight
	if endIdx > len(f.flatEntries) {
		endIdx = len(f.flatEntries)
	}

	for i := f.scrollOffset; i < endIdx; i++ {
		entry := f.flatEntries[i]
		indent := strings.Repeat("  ", entry.depth)

		icon := "  "
		if entry.isDir {
			if entry.expanded {
				icon = "▼ "
			} else {
				icon = "▶ "
			}
		}

		name := entry.name
		style := lipgloss.NewStyle().
			Width(f.width - 2).
			Foreground(t.Text())

		if i == f.selectedIdx {
			style = style.
				Background(t.BackgroundSecondary()).
				Bold(true)
		}

		if f.modifiedFiles[entry.path] {
			style = style.Foreground(t.Accent())
		}

		if entry.isDir {
			style = style.Foreground(t.Primary())
		}

		lines = append(lines, style.Render(indent+icon+name))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	return lipgloss.NewStyle().
		Width(f.width).
		Height(f.height).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(t.BorderNormal()).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, content))
}

func (f *fileTreeCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(ftKeys)
}

func (f *fileTreeCmp) SetSize(width, height int) {
	f.width = width
	f.height = height
}

func (f *fileTreeCmp) SetModifiedFiles(files map[string]bool) {
	f.modifiedFiles = files
}

func (f *fileTreeCmp) scanDir(dir string, depth, maxDepth int) []*fileEntry {
	if depth > maxDepth {
		return nil
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var entries []*fileEntry
	for _, de := range dirEntries {
		name := de.Name()
		// Skip hidden and common non-essential dirs
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "target" {
			continue
		}

		entry := &fileEntry{
			name:  name,
			path:  filepath.Join(dir, name),
			isDir: de.IsDir(),
			depth: depth,
		}

		if de.IsDir() && depth < maxDepth {
			entry.children = f.scanDir(entry.path, depth+1, maxDepth)
			entry.expanded = depth < 1
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return entries[i].name < entries[j].name
	})

	return entries
}

func (f *fileTreeCmp) flatten(entries []*fileEntry) []*fileEntry {
	var flat []*fileEntry
	for _, e := range entries {
		flat = append(flat, e)
		if e.isDir && e.expanded {
			flat = append(flat, f.flatten(e.children)...)
		}
	}
	return flat
}

// NewFileTreeCmp creates a new file tree sidebar component.
func NewFileTreeCmp() FileTreeCmp {
	return &fileTreeCmp{
		modifiedFiles: make(map[string]bool),
	}
}
