// ---
// relationships:
//   implements: DESIGN
// ---

package tui

import (
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

type styles struct {
	titleStyle, cursorStyle, mutedStyle, infoStyle, errorStyle lipgloss.Style
}

// Keep these colours in sync with the tui-* tokens in DESIGN.md.
// Bubble Tea downsamples styles at output, including NO_COLOR handling.
func newStyles(dark bool) *styles {
	choose := lipgloss.LightDark(dark)
	accent := choose(lipgloss.Color("#0b4fa8"), lipgloss.Color("#6aa5ff"))
	muted := choose(lipgloss.Color("#767676"), lipgloss.Color("#808080"))
	failure := choose(lipgloss.Color("#b42318"), lipgloss.Color("#ff5f5f"))
	return &styles{
		titleStyle:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		cursorStyle: lipgloss.NewStyle().Bold(true).Foreground(accent),
		mutedStyle:  lipgloss.NewStyle().Foreground(muted),
		infoStyle:   lipgloss.NewStyle().Foreground(accent),
		errorStyle:  lipgloss.NewStyle().Foreground(failure),
	}
}

// COLORFGBG provides a fallback for terminals without background query support.
func defaultStyles() *styles {
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	bg, err := strconv.Atoi(parts[len(parts)-1])
	dark := err != nil || bg < 7 || bg == 8
	return newStyles(dark)
}

func (m *Model) palette() *styles {
	if m.styles == nil {
		m.styles = defaultStyles()
	}
	return m.styles
}

func (m pickerModel) palette() *styles {
	if m.styles == nil {
		return defaultStyles()
	}
	return m.styles
}
