package toast

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
)

// ToastType defines the type of toast notification.
type ToastType int

const (
	ToastSuccess ToastType = iota
	ToastError
	ToastInfo
	ToastWarning
)

// ShowToastMsg triggers a new toast notification.
type ShowToastMsg struct {
	Type    ToastType
	Message string
	TTL     time.Duration
}

type dismissToastMsg struct {
	id int
}

type toastEntry struct {
	id      int
	ttype   ToastType
	message string
}

// ToastCmp manages transient notification toasts.
type ToastCmp interface {
	tea.Model
	SetSize(width, height int)
}

type toastCmp struct {
	toasts   []toastEntry
	nextID   int
	width    int
	height   int
}

func (t *toastCmp) Init() tea.Cmd {
	return nil
}

func (t *toastCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ShowToastMsg:
		id := t.nextID
		t.nextID++
		entry := toastEntry{
			id:      id,
			ttype:   msg.Type,
			message: msg.Message,
		}
		t.toasts = append(t.toasts, entry)

		// Max 5 toasts
		if len(t.toasts) > 5 {
			t.toasts = t.toasts[1:]
		}

		ttl := msg.TTL
		if ttl == 0 {
			ttl = 3 * time.Second
		}
		return t, tea.Tick(ttl, func(time.Time) tea.Msg {
			return dismissToastMsg{id: id}
		})

	case dismissToastMsg:
		for i, toast := range t.toasts {
			if toast.id == msg.id {
				t.toasts = append(t.toasts[:i], t.toasts[i+1:]...)
				break
			}
		}
	}
	return t, nil
}

func (t *toastCmp) View() string {
	th := theme.CurrentTheme()
	if th == nil || len(t.toasts) == 0 {
		return ""
	}

	var rendered []string
	for _, toast := range t.toasts {
		var fg, bg lipgloss.AdaptiveColor
		var icon string

		switch toast.ttype {
		case ToastSuccess:
			fg = th.Background()
			bg = th.Success()
			icon = "✓"
		case ToastError:
			fg = th.Background()
			bg = th.Error()
			icon = "✗"
		case ToastInfo:
			fg = th.Background()
			bg = th.Info()
			icon = "ℹ"
		case ToastWarning:
			fg = th.Background()
			bg = th.Warning()
			icon = "⚠"
		}

		style := lipgloss.NewStyle().
			Foreground(fg).
			Background(bg).
			Padding(0, 1).
			MaxWidth(40)

		rendered = append(rendered, style.Render(icon+" "+toast.message))
	}

	return lipgloss.JoinVertical(lipgloss.Right, rendered...)
}

func (t *toastCmp) SetSize(width, height int) {
	t.width = width
	t.height = height
}

// NewToastCmp creates a new toast notification manager.
func NewToastCmp() ToastCmp {
	return &toastCmp{}
}
