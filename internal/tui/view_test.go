package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestListRendersPRStateAndReportsSkippedRecords(t *testing.T) {
	open := model.PR2{Schema: 2, ID: "01BRANCHOPEN00000000000000", SourceBranch: "topic", Title: "Open work", State: model.PRStateOpen}
	closed := model.PR2{Schema: 2, ID: "01BRANCHCLOSED000000000000", SourceBranch: "old", Title: "Old work", State: model.PRStateClosed, Closure: &model.Closure{Reason: model.ClosureAbandoned}}
	m := &Model{width: 100, height: 30, allRecords: []model.PR2{open, closed}, skipped: 2}
	m.applyListMode()

	view := ansi.Strip(m.renderList())
	if !strings.Contains(view, "01BRANCHOPEN  open") {
		t.Errorf("open record row missing:\n%s", view)
	}
	if strings.Contains(view, "01BRANCHCLOS") {
		t.Errorf("closed record listed in the open view:\n%s", view)
	}
	if !strings.Contains(view, "Skipped 2") || !strings.Contains(view, "Legacy records") {
		t.Errorf("skip line missing:\n%s", view)
	}

	m.showAll = true
	m.applyListMode()
	all := ansi.Strip(m.renderList())
	if !strings.Contains(all, "closed (abandoned)") {
		t.Errorf("all view missing closure reason:\n%s", all)
	}
}

func TestDetailViewIsReadOnlyAndPointsActionsAtTheCLI(t *testing.T) {
	pr := model.PR2{Schema: 2, ID: "01BRANCHDETAIL000000000000", SourceBranch: "topic", BaseBranch: "main", Title: "Detail", State: model.PRStateOpen}
	m := &Model{width: 100, height: 30, screen: detailScreen, currentPR: &pr}
	for _, key := range []string{"c", "r", "m"} {
		m.infoMessage = ""
		if _, cmd := m.handleDetailKeys(testKey(key)); cmd != nil {
			t.Fatalf("key %q returned a command; the detail view must not mutate", key)
		}
		if !strings.Contains(m.infoMessage, "gitpr review") {
			t.Errorf("key %q guidance = %q", key, m.infoMessage)
		}
	}
	view := ansi.Strip(m.renderDetail())
	if !strings.Contains(view, "Read-only PR view") || !strings.Contains(view, "Latest event: none") {
		t.Errorf("detail view:\n%s", view)
	}
}

func testKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
