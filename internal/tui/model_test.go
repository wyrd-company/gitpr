package tui

import (
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestListLoadedReportsSkippedBranchBasedRecords(t *testing.T) {
	m := &Model{}
	updated, _ := m.Update(listLoadedMsg{prs: []model.PR{{ID: "legacy"}}, skipped: 2})
	got := updated.(*Model)
	if len(got.openPRs) != 1 || !strings.Contains(got.infoMessage, "2 branch-based") || !strings.Contains(got.infoMessage, "increment 8") {
		t.Fatalf("TUI skipped-record state = prs %d, message %q", len(got.openPRs), got.infoMessage)
	}
}

func TestPRLoadedResetsCommentCycle(t *testing.T) {
	m := &Model{
		editingCommentIndex: 0,
		commentCycle: commentCycleState{
			target: commentTarget{filePath: "app.txt", lineStart: 2, lineEnd: 2},
			next:   2,
		},
	}

	updated, _ := m.Update(prLoadedMsg{pr: model.PR{ID: "test"}})
	got := updated.(*Model)

	if got.editingCommentIndex != -1 {
		t.Fatalf("editingCommentIndex = %d, want -1", got.editingCommentIndex)
	}
	if got.commentCycle != (commentCycleState{}) {
		t.Fatalf("commentCycle = %+v, want zero value", got.commentCycle)
	}
}

func TestCommentSavedResetsCommentCycle(t *testing.T) {
	m := &Model{
		currentPR:           model.PR{ID: "test", Status: model.StatusOpen},
		editingCommentIndex: 1,
		commentCycle: commentCycleState{
			target: commentTarget{filePath: "app.txt", lineStart: 2, lineEnd: 2},
			next:   1,
		},
	}

	updated, _ := m.Update(actionResultMsg{
		pr:           model.PR{ID: "test", Status: model.StatusOpen},
		message:      "status text that must not drive control flow",
		commentSaved: true,
	})
	got := updated.(*Model)

	if got.editingCommentIndex != -1 {
		t.Fatalf("editingCommentIndex = %d, want -1", got.editingCommentIndex)
	}
	if got.commentCycle != (commentCycleState{}) {
		t.Fatalf("commentCycle = %+v, want zero value", got.commentCycle)
	}
}

func TestCommentSavedFalsePreservesCommentCycle(t *testing.T) {
	cycle := commentCycleState{
		target: commentTarget{filePath: "app.txt", lineStart: 2, lineEnd: 2},
		next:   1,
	}
	m := &Model{
		currentPR:           model.PR{ID: "test", Status: model.StatusOpen},
		editingCommentIndex: 1,
		commentCycle:        cycle,
	}

	updated, _ := m.Update(actionResultMsg{
		pr:           model.PR{ID: "test", Status: model.StatusOpen},
		message:      "PR 01K0 merged into main",
		commentSaved: false,
	})
	got := updated.(*Model)

	if got.editingCommentIndex != 1 {
		t.Fatalf("editingCommentIndex = %d, want 1", got.editingCommentIndex)
	}
	if got.commentCycle != cycle {
		t.Fatalf("commentCycle = %+v, want %+v", got.commentCycle, cycle)
	}
}
