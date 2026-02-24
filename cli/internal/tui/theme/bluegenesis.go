package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// BlueGenesisTheme implements the Theme interface with the BlueGenesis color palette.
// Electric blue primary, cyan accent, deep blue backgrounds — the signature look.
type BlueGenesisTheme struct {
	BaseTheme
}

// NewBlueGenesisTheme creates a new instance of the BlueGenesis theme.
func NewBlueGenesisTheme() *BlueGenesisTheme {
	// BlueGenesis dark palette
	darkPrimary := "#00A8FF"    // electric blue
	darkSecondary := "#0D47A1"  // deep blue
	darkAccent := "#00E5FF"     // cyan spark
	darkBackground := "#0A0E14" // near-black blue
	darkBgSecondary := "#121A24"
	darkBgDarker := "#060A10"
	darkText := "#E0E0E0"
	darkTextMuted := "#6E7A8A"
	darkTextEmph := "#FFFFFF"
	darkBorder := "#1C2A3A"
	darkBorderFocused := "#00A8FF"
	darkBorderDim := "#0F1820"
	darkSuccess := "#00E676"
	darkError := "#FF5252"
	darkWarning := "#FFD740"
	darkInfo := "#40C4FF"

	// Light mode palette
	lightPrimary := "#0077CC"
	lightSecondary := "#0D47A1"
	lightAccent := "#0097A7"
	lightBackground := "#F5F8FC"
	lightBgSecondary := "#E8EEF5"
	lightBgDarker := "#FFFFFF"
	lightText := "#1A2A3A"
	lightTextMuted := "#607080"
	lightTextEmph := "#0A1520"
	lightBorder := "#C0D0E0"
	lightBorderFocused := "#0077CC"
	lightBorderDim := "#D8E4F0"
	lightSuccess := "#00C853"
	lightError := "#D32F2F"
	lightWarning := "#F9A825"
	lightInfo := "#0288D1"

	t := &BlueGenesisTheme{}

	// Base colors
	t.PrimaryColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.SecondaryColor = lipgloss.AdaptiveColor{Dark: darkSecondary, Light: lightSecondary}
	t.AccentColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}

	// Status colors
	t.ErrorColor = lipgloss.AdaptiveColor{Dark: darkError, Light: lightError}
	t.WarningColor = lipgloss.AdaptiveColor{Dark: darkWarning, Light: lightWarning}
	t.SuccessColor = lipgloss.AdaptiveColor{Dark: darkSuccess, Light: lightSuccess}
	t.InfoColor = lipgloss.AdaptiveColor{Dark: darkInfo, Light: lightInfo}

	// Text colors
	t.TextColor = lipgloss.AdaptiveColor{Dark: darkText, Light: lightText}
	t.TextMutedColor = lipgloss.AdaptiveColor{Dark: darkTextMuted, Light: lightTextMuted}
	t.TextEmphasizedColor = lipgloss.AdaptiveColor{Dark: darkTextEmph, Light: lightTextEmph}

	// Background colors
	t.BackgroundColor = lipgloss.AdaptiveColor{Dark: darkBackground, Light: lightBackground}
	t.BackgroundSecondaryColor = lipgloss.AdaptiveColor{Dark: darkBgSecondary, Light: lightBgSecondary}
	t.BackgroundDarkerColor = lipgloss.AdaptiveColor{Dark: darkBgDarker, Light: lightBgDarker}

	// Border colors
	t.BorderNormalColor = lipgloss.AdaptiveColor{Dark: darkBorder, Light: lightBorder}
	t.BorderFocusedColor = lipgloss.AdaptiveColor{Dark: darkBorderFocused, Light: lightBorderFocused}
	t.BorderDimColor = lipgloss.AdaptiveColor{Dark: darkBorderDim, Light: lightBorderDim}

	// Diff view colors — tuned to blue palette
	t.DiffAddedColor = lipgloss.AdaptiveColor{Dark: darkSuccess, Light: lightSuccess}
	t.DiffRemovedColor = lipgloss.AdaptiveColor{Dark: darkError, Light: lightError}
	t.DiffContextColor = lipgloss.AdaptiveColor{Dark: darkTextMuted, Light: lightTextMuted}
	t.DiffHunkHeaderColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.DiffHighlightAddedColor = lipgloss.AdaptiveColor{Dark: "#69F0AE", Light: "#A5D6A7"}
	t.DiffHighlightRemovedColor = lipgloss.AdaptiveColor{Dark: "#FF8A80", Light: "#EF9A9A"}
	t.DiffAddedBgColor = lipgloss.AdaptiveColor{Dark: "#0A1F14", Light: "#E8F5E9"}
	t.DiffRemovedBgColor = lipgloss.AdaptiveColor{Dark: "#1F0A0A", Light: "#FFEBEE"}
	t.DiffContextBgColor = lipgloss.AdaptiveColor{Dark: darkBackground, Light: lightBackground}
	t.DiffLineNumberColor = lipgloss.AdaptiveColor{Dark: darkTextMuted, Light: lightTextMuted}
	t.DiffAddedLineNumberBgColor = lipgloss.AdaptiveColor{Dark: "#081A12", Light: "#C8E6C9"}
	t.DiffRemovedLineNumberBgColor = lipgloss.AdaptiveColor{Dark: "#1A0808", Light: "#FFCDD2"}

	// Markdown colors
	t.MarkdownTextColor = lipgloss.AdaptiveColor{Dark: darkText, Light: lightText}
	t.MarkdownHeadingColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.MarkdownLinkColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.MarkdownLinkTextColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.MarkdownCodeColor = lipgloss.AdaptiveColor{Dark: "#80D8FF", Light: "#01579B"}
	t.MarkdownBlockQuoteColor = lipgloss.AdaptiveColor{Dark: darkWarning, Light: lightWarning}
	t.MarkdownEmphColor = lipgloss.AdaptiveColor{Dark: darkWarning, Light: lightWarning}
	t.MarkdownStrongColor = lipgloss.AdaptiveColor{Dark: darkTextEmph, Light: lightTextEmph}
	t.MarkdownHorizontalRuleColor = lipgloss.AdaptiveColor{Dark: darkBorder, Light: lightBorder}
	t.MarkdownListItemColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.MarkdownListEnumerationColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.MarkdownImageColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.MarkdownImageTextColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.MarkdownCodeBlockColor = lipgloss.AdaptiveColor{Dark: darkText, Light: lightText}

	// Syntax highlighting colors
	t.SyntaxCommentColor = lipgloss.AdaptiveColor{Dark: "#546E7A", Light: "#78909C"}
	t.SyntaxKeywordColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	t.SyntaxFunctionColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.SyntaxVariableColor = lipgloss.AdaptiveColor{Dark: "#82B1FF", Light: "#1565C0"}
	t.SyntaxStringColor = lipgloss.AdaptiveColor{Dark: darkSuccess, Light: lightSuccess}
	t.SyntaxNumberColor = lipgloss.AdaptiveColor{Dark: "#FF9E80", Light: "#E65100"}
	t.SyntaxTypeColor = lipgloss.AdaptiveColor{Dark: "#B388FF", Light: "#7C4DFF"}
	t.SyntaxOperatorColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	t.SyntaxPunctuationColor = lipgloss.AdaptiveColor{Dark: darkText, Light: lightText}

	return t
}

func init() {
	RegisterTheme("bluegenesis", NewBlueGenesisTheme())
}
