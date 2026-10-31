// ---
// relationships:
//
//	verifies: DESIGN
//
// ---
package tui

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/wyrd-company/gitpr/internal/model"
)

func TestV2ViewsAlwaysUseAlternateScreen(t *testing.T) {
	for _, m := range []tea.Model{&Model{}, &Model{width: 80, height: 24}, pickerModel{title: "Choose a PR"}} {
		if v := m.View(); !v.AltScreen || v.Content == "" {
			t.Fatalf("%T loses initial/empty view: %#v", m, v)
		}
	}
}

func TestPickerPressSelectCancelAndRelease(t *testing.T) {
	prs := []model.PR2{{ID: "first"}, {ID: "second"}}
	m := pickerModel{prs: prs}
	next, cmd := m.Update(tea.KeyReleaseMsg{Code: 'j'})
	if cmd != nil || next.(pickerModel).cursor != 0 {
		t.Fatal("release must not navigate")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyUp}, testKey("j")} {
		next, _ = m.Update(key)
		m = next.(pickerModel)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next.(pickerModel).selected != "second" || cmd == nil {
		t.Fatal("selection failed")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, testKey("q"), {Code: 'c', Mod: tea.ModCtrl}} {
		next, cmd = m.Update(key)
		if !next.(pickerModel).canceled || cmd == nil {
			t.Fatal("cancel failed")
		}
	}
}

func TestV2BackgroundResponsesAreModelLocal(t *testing.T) {
	dark, light := &Model{}, pickerModel{}
	dark.Update(tea.BackgroundColorMsg{Color: color.RGBA{A: 255}})
	next, _ := light.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	light = next.(pickerModel)
	for _, tc := range []struct{ got, want string }{
		{dark.palette().titleStyle.Render("Example"), "106;165;255"},
		{light.palette().titleStyle.Render("Example"), "11;79;168"},
	} {
		if !strings.Contains(tc.got, tc.want) {
			t.Fatalf("background response: %q", tc.got)
		}
	}
}

func TestSpacePressPagesDiff(t *testing.T) {
	report := constructedReview()
	m := &Model{width: 80, height: 8, screen: diffScreen, review: &report}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.diffOffset == 0 {
		t.Fatal("space did not page")
	}
}

// A cancelled program must propagate its error after closing the alternate screen.
func TestPickerProgramRestoresScreenOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &cancelOutput{cancel: cancel}
	p := tea.NewProgram(pickerModel{title: "Choose a PR"}, tea.WithInput(nil), tea.WithOutput(output), tea.WithWindowSize(80, 24), tea.WithContext(ctx))
	_, err := p.Run()
	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("cancellation error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "\x1b[?1049h") || !strings.Contains(got, "\x1b[?1049l") {
		t.Fatalf("screen not restored: %q", got)
	}
}

type cancelOutput struct {
	strings.Builder
	cancel context.CancelFunc
}

func (w *cancelOutput) Write(p []byte) (int, error) {
	n, err := w.Builder.Write(p)
	if bytes.Contains(p, []byte("\x1b[?1049h")) {
		w.cancel()
	}
	return n, err
}

func TestListOnlyNavigatesOnPress(t *testing.T) {
	m := &Model{openRecords: []model.PR2{{ID: "first"}, {ID: "second"}}}
	m.Update(tea.KeyReleaseMsg{Code: 'j'})
	if m.listCursor != 0 {
		t.Fatal("release navigated list")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.listCursor != 1 {
		t.Fatal("down did not navigate")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.listCursor != 0 {
		t.Fatal("up did not navigate")
	}
	for _, key := range []tea.KeyPressMsg{testKey("q"), {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatal("quit ignored")
		}
	}
}
