// ---
// relationships:
//   implements: DESIGN
// ---

package tui

import "github.com/charmbracelet/lipgloss"

// Keep these colours in sync with the tui-* tokens in DESIGN.md.
var (
	accentColor = lipgloss.AdaptiveColor{Dark: "#6aa5ff", Light: "#0b4fa8"}
	mutedColor  = lipgloss.AdaptiveColor{Dark: "#808080", Light: "#767676"}
	errorColor  = lipgloss.AdaptiveColor{Dark: "#ff5f5f", Light: "#b42318"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	mutedStyle  = lipgloss.NewStyle().Foreground(mutedColor)
	infoStyle   = lipgloss.NewStyle().Foreground(accentColor)
	errorStyle  = lipgloss.NewStyle().Foreground(errorColor)
)
