package status

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/tui/theme"
)

// CostInfo holds token usage and cost data.
type CostInfo struct {
	InputTokens  int64
	OutputTokens int64
	CacheHits    int64
	Cost         float64
}

// FormatTokenCount formats a token count in human-readable format.
func FormatTokenCount(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

// RenderCostBadge renders the cost display for the status bar.
func RenderCostBadge(info CostInfo) string {
	t := theme.CurrentTheme()
	if t == nil {
		return ""
	}

	inputStr := FormatTokenCount(info.InputTokens)
	outputStr := FormatTokenCount(info.OutputTokens)

	costStyle := lipgloss.NewStyle().
		Foreground(t.Text())

	return costStyle.Render(
		fmt.Sprintf("$%.2f | ↑%s ↓%s", info.Cost, inputStr, outputStr),
	)
}
