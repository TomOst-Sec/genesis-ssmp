package brand

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/genesis-ssmp/genesis/cli/internal/version"
)

// ASCII art logo for BlueGenesis
const Logo = `
   ██████╗ ███████╗███╗   ██╗███████╗███████╗██╗███████╗
  ██╔════╝ ██╔════╝████╗  ██║██╔════╝██╔════╝██║██╔════╝
  ██║  ███╗█████╗  ██╔██╗ ██║█████╗  ███████╗██║███████╗
  ██║   ██║██╔══╝  ██║╚██╗██║██╔══╝  ╚════██║██║╚════██║
  ╚██████╔╝███████╗██║ ╚████║███████╗███████║██║███████║
   ╚═════╝ ╚══════╝╚═╝  ╚═══╝╚══════╝╚══════╝╚═╝╚══════╝`

const Tagline = "Sovereign Software Manufacturing Plant"

// LogoLines returns the logo split into individual lines (without leading blank line).
func LogoLines() []string {
	lines := strings.Split(Logo, "\n")
	// Trim leading empty line
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

// RenderLogo renders the ASCII logo with blue gradient colors.
func RenderLogo() string {
	lines := LogoLines()

	// Blue gradient from deep blue to cyan
	gradientColors := []string{
		"#0D47A1", // deep blue
		"#1565C0",
		"#1E88E5",
		"#42A5F5",
		"#00A8FF", // electric blue (primary)
		"#29B6F6",
		"#00E5FF", // cyan spark (accent)
	}

	var rendered []string
	for i, line := range lines {
		colorIdx := i
		if colorIdx >= len(gradientColors) {
			colorIdx = len(gradientColors) - 1
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(gradientColors[colorIdx]))
		rendered = append(rendered, style.Render(line))
	}

	return strings.Join(rendered, "\n")
}

// RenderLogoWithVersion renders the full logo block with version info.
func RenderLogoWithVersion() string {
	logo := RenderLogo()

	tagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6E7A8A")).
		Italic(true)

	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00A8FF")).
		Bold(true)

	return fmt.Sprintf("%s\n\n  %s\n  %s\n",
		logo,
		tagStyle.Render(Tagline),
		versionStyle.Render("v"+version.Version),
	)
}

// RenderCompact renders a compact single-line brand.
func RenderCompact() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00A8FF")).
		Bold(true)
	muted := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6E7A8A"))

	return fmt.Sprintf("%s %s",
		style.Render("Genesis"),
		muted.Render("v"+version.Version),
	)
}
