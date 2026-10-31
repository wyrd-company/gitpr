// ---
// relationships:
//   verifies: DESIGN
// ---

package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/wyrd-company/gitpr/internal/model"
)

func TestScreensKeepContentWithoutColour(t *testing.T) {
	pr := model.PR2{ID: "01EXAMPLE000000000000000000", Title: "Example change", State: model.PRStateOpen, SourceBranch: "topic", BaseBranch: "main"}
	m := &Model{width: 100, height: 30, openRecords: []model.PR2{pr}, currentPR: &pr, infoMessage: "Use gitpr review", errMessage: "Unable to load record"}
	picker := pickerModel{title: "Choose a PR", prs: []model.PR2{pr}}
	screens := []struct {
		name   string
		render func() string
		want   []string
	}{
		{"list", m.renderList, []string{"Open PRs", "> 01EXAMPLE000", "Example change", "Keys:", m.errMessage}},
		{"detail", m.renderDetail, []string{"PR 01EXAMPLE000", "Example change", "Read-only PR view", m.infoMessage, m.errMessage}},
		{"picker", picker.content, []string{"Choose a PR", "> 01EXAMPLE000", "Example change", "esc cancel"}},
		{"empty", (&Model{}).renderList, []string{"Open PRs", "No open PRs", "Keys:"}},
	}
	for _, screen := range screens {
		t.Run(screen.name, func(t *testing.T) {
			coloured := screen.render()
			var output strings.Builder
			w := &colorprofile.Writer{Forward: &output, Profile: colorprofile.NoTTY}
			if _, err := w.WriteString(coloured); err != nil {
				t.Fatal(err)
			}
			plain := output.String()
			if strings.Contains(plain, "\x1b[") {
				t.Fatalf("colour-off screen contains escapes: %q", plain)
			}
			if plain != ansi.Strip(coloured) {
				t.Fatalf("colour-off screen lost content: %q", plain)
			}
			for _, want := range screen.want {
				if !strings.Contains(plain, want) {
					t.Errorf("missing %q in %q", want, plain)
				}
			}
		})
	}
}

func TestAdaptiveAccentMatchesTerminalBackground(t *testing.T) {
	for _, tc := range []struct {
		dark bool
		rgb  string
	}{{true, "106;165;255"}, {false, "11;79;168"}} {
		palette := newStyles(tc.dark)
		for _, style := range []lipgloss.Style{palette.titleStyle, palette.cursorStyle, palette.infoStyle} {
			if got := style.Render("Example"); !strings.Contains(got, "38;2;"+tc.rgb) {
				t.Errorf("dark=%v: expected brand accent, got %q", tc.dark, got)
			}
		}
	}
}
