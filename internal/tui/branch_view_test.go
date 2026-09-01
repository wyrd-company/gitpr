package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestMainListRendersLegacyUnchangedAndBranchRecordWithStateReason(t *testing.T) {
	legacy := model.PR{ID: "01LEGACYTUILIST00000000000", SourceBranch: "legacy-topic", Title: "Legacy title", Status: model.StatusOpen}
	branch := model.PR2{Schema: 2, ID: "01BRANCHTUILIST00000000000", SourceBranch: "branch-topic", Title: "Branch title", State: model.PRStateClosed, Closure: &model.Closure{Reason: model.ClosureAbandoned}}
	m := &Model{width: 100, height: 30, openRecords: []model.Record{legacy, branch}}
	view := ansi.Strip(m.renderList())
	legacyLine := "> 01LEGACYTUIL  legacy-topic       Legacy title"
	if !strings.Contains(view, legacyLine) {
		t.Fatalf("legacy row changed or missing %q:\n%s", legacyLine, view)
	}
	for _, want := range []string{"01BRANCHTUIL", "closed (abandoned)", "branch-topic", "Branch title"} {
		if !strings.Contains(view, want) {
			t.Errorf("branch row missing %q:\n%s", want, view)
		}
	}
}

func TestBranchDetailRendersReviewThreadsAndClosureEvidence(t *testing.T) {
	pr := model.PR2{Schema: 2, ID: "01BRANCHDETAIL000000000000", Title: "Read only", SourceBranch: "topic", BaseBranch: "main", State: model.PRStateClosed,
		Events:  []model.ReviewEvent{{ID: "01EVENTDETAIL0000000000000", Verdict: model.VerdictAccepted, SourceHeadSHA: strings.Repeat("a", 40), BaseHeadSHA: strings.Repeat("b", 40)}},
		Threads: []model.Thread{{ID: "open", Status: model.ThreadOpen, Outdated: true}, {ID: "resolved", Status: model.ThreadResolved}},
		Closure: &model.Closure{Reason: model.ClosureIntegrated, DestinationBranch: "release", ResultingCommitSHAs: []string{strings.Repeat("c", 40)}, PatchEquivalentIdentities: []string{"patch-one"}, Note: "landed elsewhere"},
	}
	m := &Model{width: 120, height: 30, currentPR2: &pr}
	view := ansi.Strip(m.renderDetail())
	for _, want := range []string{"State: closed", "Latest event: 01EVENTDETAIL", "Verdict: accepted", strings.Repeat("a", 40) + " / " + strings.Repeat("b", 40), "Threads: 1 open, 1 resolved, 1 outdated", "Closure reason: integrated", "Destination: release", "Patch identities: patch-one", "Closure note: landed elsewhere"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail missing %q:\n%s", want, view)
		}
	}
}

func TestBranchDetailActionsShowCLIFlowWithoutMutationCommand(t *testing.T) {
	pr := model.PR2{Schema: 2, ID: "01BRANCHGUIDANCE0000000000", State: model.PRStateOpen}
	for _, key := range []string{"c", "r", "m"} {
		m := &Model{screen: detailScreen, mode: modeBrowse, currentPR2: &pr}
		updated, cmd := m.handleDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		got := updated.(*Model)
		if cmd != nil || !strings.Contains(got.infoMessage, "gitpr review <id>") || !strings.Contains(got.infoMessage, "approve/reject with its basis") || !strings.Contains(got.infoMessage, "gitpr merge <id>") || !strings.Contains(got.infoMessage, "gitpr comment <id>") {
			t.Fatalf("key %s guidance=%q cmd=%v", key, got.infoMessage, cmd)
		}
	}
}
