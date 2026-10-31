// relationships:
//
//	tests: adopt-branch-based-pull-requests
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
)

func constructedReview() app.ReviewReport {
	contained := false
	return app.ReviewReport{
		ID: "01EXAMPLE000000000000000000", SourceBranch: "topic", BaseBranch: "main",
		Basis: app.ReviewBasis{SourceHeadSHA: "live-source", BaseHeadSHA: "live-base", MergeBaseSHA: "merge-base", BaseContained: &contained},
		Diff: []model.FileDiff{
			{OldPath: "old.txt", NewPath: "new.txt", Status: "R", Hunks: []model.Hunk{
				{OldStart: 10, OldLines: 2, NewStart: 10, NewLines: 2, Header: "section", Lines: []model.DiffLine{
					{Kind: "context", OldLine: 10, NewLine: 10, Content: "same"},
					{Kind: "delete", OldLine: 11, Content: "before"},
					{Kind: "add", NewLine: 11, Content: "after"},
				}},
				{OldStart: 30, OldLines: 0, NewStart: 31, NewLines: 1, Lines: []model.DiffLine{{Kind: "add", NewLine: 31, Content: "another hunk"}}},
			}},
			{NewPath: "image.bin", Status: "A", Patch: "Binary files differ"},
			{OldPath: "removed.txt", Status: "D"},
		},
		Threads: []model.Thread{
			{ID: "live-thread", Kind: model.ThreadAnchored, Status: model.ThreadOpen, Anchor: &model.ThreadAnchor{File: "new.txt", Side: model.DiffSideSource, LineStart: 11, LineEnd: 12, SourceHeadSHA: "live-source", BaseHeadSHA: "live-base"}, Comments: []model.ThreadComment{{Body: "first\nsecond"}}},
			{ID: "old-thread", Kind: model.ThreadAnchored, Status: model.ThreadResolved, Outdated: true, Anchor: &model.ThreadAnchor{File: "old.txt", Side: model.DiffSideBase, LineStart: 9, LineEnd: 9, SourceHeadSHA: "old-source", BaseHeadSHA: "old-base"}, Comments: []model.ThreadComment{{Body: "old discussion", PostClosure: true}}},
			{ID: "general-thread", Kind: model.ThreadPRLevel, Status: model.ThreadOpen, Comments: []model.ThreadComment{{Body: "general discussion"}}},
		},
	}
}

func TestRenderLiveReviewHunksBasisAndThreads(t *testing.T) {
	report := constructedReview()
	view := renderReview(&report)
	for _, want := range []string{
		"Source head: live-source", "Base head: live-base", "Merge base: merge-base", "Base contained in source: false",
		"File: old.txt -> new.txt [R]", "@@ -10,2 +10,2 @@ section", "@@ -30,0 +31,1 @@", "   10    10  same", "   11       -before", "         11 +after", "+another hunk",
		"image.bin", "Binary files differ", "removed.txt", "No text hunks",
		"Thread live-thread [anchored, open, outdated: false]", "Anchor: new.txt source:11-12", "first\nsecond",
		"Thread old-thread [anchored, resolved, outdated: true]", "Anchor: old.txt base:9-9", "Anchor basis: old-source / old-base", "post-closure: true", "old discussion",
		"Thread general-thread [pr-level, open, outdated: false]", "general discussion",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
}

func TestReviewEmptyAndMissingBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		basis app.ReviewBasis
		want  string
	}{
		{"empty", app.ReviewBasis{}, "No changed files."},
		{"source missing", app.ReviewBasis{SourceBranchMissing: true}, "Source branch missing."},
		{"base missing", app.ReviewBasis{BaseBranchMissing: true}, "Base branch missing."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := renderReview(&app.ReviewReport{Basis: tc.basis})
			if !strings.Contains(view, tc.want) {
				t.Fatal(view)
			}
			if tc.name != "empty" && strings.Contains(view, "No changed files.") {
				t.Fatal("missing branch presented as empty diff")
			}
		})
	}
}

func TestDiffNavigationRefreshAndReadOnlyActions(t *testing.T) {
	pr := model.PR2{ID: "example", Threads: []model.Thread{{ID: "stored-thread"}}}
	m := &Model{width: 70, height: 15, screen: detailScreen, currentPR: &pr}
	_, cmd := m.handleDetailKeys(testKey("d"))
	if cmd == nil {
		t.Fatal("diff key must request live review")
	}
	report := constructedReview()
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report})
	if m.screen != diffScreen || m.diffOffset != 0 {
		t.Fatal("report did not open diff")
	}
	if strings.Contains(m.renderDiff(), "stored-thread") {
		t.Fatal("used stored threads")
	}
	for _, key := range []string{"c", "r", "m"} {
		if _, cmd := m.handleDiffKeys(testKey(key)); cmd != nil {
			t.Fatalf("write command from %s", key)
		}
		if !strings.Contains(m.infoMessage, "gitpr review") {
			t.Fatal("missing CLI guidance")
		}
	}
	m.handleDiffKeys(testKey("G"))
	if m.diffOffset == 0 || !strings.Contains(m.renderDiff(), "general discussion") {
		t.Fatal("cannot reach final thread")
	}
	m.handleDiffKeys(testKey("j"))
	if m.diffOffset != len(m.diffLines())-m.diffPageSize() {
		t.Fatal("scrolled past end")
	}
	m.handleDiffKeys(testKey("g"))
	m.handleDiffKeys(testKey("k"))
	if m.diffOffset != 0 {
		t.Fatal("scrolled before start")
	}
	m.handleDiffKeys(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.diffOffset != m.diffPageSize() {
		t.Fatal("page down")
	}
	m.handleDiffKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.diffOffset != 0 {
		t.Fatal("page up")
	}
	for _, line := range m.diffLines() {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("unwrapped line: %q", line)
		}
	}
	_, cmd = m.handleDiffKeys(testKey("R"))
	if cmd == nil {
		t.Fatal("refresh did not request report")
	}
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, err: errors.New("review unavailable")})
	if m.review.Basis.SourceHeadSHA != "live-source" || m.errMessage != "review unavailable" {
		t.Fatal("failed refresh lost report or error")
	}
	m.handleDiffKeys(testKey("R"))
	report.Basis.SourceHeadSHA = "refreshed-source"
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report})
	if m.review.Basis.SourceHeadSHA != "refreshed-source" || m.errMessage != "" {
		t.Fatal("refresh failed")
	}
	m.handleDiffKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != detailScreen || m.currentPR != &pr {
		t.Fatal("escape lost detail")
	}
	m.handleDetailKeys(testKey("d"))
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, err: errors.New("failed load")})
	if m.screen != detailScreen || m.errMessage != "failed load" {
		t.Fatal("initial failure left detail")
	}
}

func TestDiffViewportFitsAfterResize(t *testing.T) {
	report := constructedReview()
	m := &Model{width: 100, height: 30, screen: diffScreen, review: &report, infoMessage: strings.Repeat("guidance ", 30), errMessage: "error\nsecond line"}
	m.handleDiffKeys(testKey("G"))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	view := m.View()
	if len(strings.Split(view, "\n")) > m.height {
		t.Fatalf("viewport exceeds height:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("viewport exceeds width: %q", line)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 100})
	m.View()
	if m.diffOffset != 0 {
		t.Fatal("resize left offset beyond content")
	}
}

func TestReviewResponseAfterLeavingRecordIsIgnored(t *testing.T) {
	report := constructedReview()
	m := &Model{screen: listScreen}
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: report.ID, report: report})
	if m.screen != listScreen || m.review != nil {
		t.Fatal("late report reopened record")
	}
	m.currentPR = &model.PR2{ID: "other-record"}
	m.screen = detailScreen
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: report.ID, err: errors.New("old request error")})
	m.Update(reviewLoadedMsg{request: m.reviewRequest, id: report.ID, report: report})
	if m.screen != detailScreen || m.review != nil || m.errMessage != "" {
		t.Fatal("late response affected another record")
	}
}

// Deliver constructed responses in a controlled order through Update, the
// Bubble Tea message boundary. No sleeps or service timing are needed.
func TestRefreshAfterEscapeDoesNotReopenDiff(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint("error=", failed), func(t *testing.T) {
			report := constructedReview()
			pr := model.PR2{ID: report.ID}
			m := &Model{screen: diffScreen, currentPR: &pr, review: &report}
			_, cmd := m.Update(testKey("R"))
			if cmd == nil || m.reviewRequest == nil {
				t.Fatal("refresh was not started")
			}
			response := reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report}
			if failed {
				response.err = errors.New("late refresh error")
			}
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if m.reviewRequest != nil {
				t.Fatal("Escape retained pending request")
			}
			m.Update(response)
			if m.screen != detailScreen || m.review != &report || m.errMessage != "" {
				t.Fatal("late refresh changed detail after Escape")
			}
		})
	}
}

func TestOutOfOrderReviewResponsesKeepLatestRequest(t *testing.T) {
	for _, order := range []string{"older-first", "newer-first"} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/older-error=%t", order, failed), func(t *testing.T) {
				report := constructedReview()
				pr := model.PR2{ID: report.ID}
				m := &Model{screen: diffScreen, currentPR: &pr, review: &report, diffOffset: 3}
				m.Update(testKey("R"))
				older := reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report}
				if failed {
					older.err = errors.New("superseded error")
				}
				m.Update(testKey("R"))
				newerReport := report
				newerReport.Basis.SourceHeadSHA = "newer-source"
				newer := reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: newerReport}
				if older.request == newer.request {
					t.Fatal("requests share an identity")
				}
				if order == "older-first" {
					m.Update(older)
					if m.review != &report || m.diffOffset != 3 || m.errMessage != "" {
						t.Fatal("superseded response changed report while latest pending")
					}
				}
				m.Update(newer)
				m.Update(testKey("j"))
				offset := m.diffOffset
				m.infoMessage = "retained guidance"
				m.Update(older)
				// A request settles once, including a duplicate response.
				m.Update(newer)
				if m.screen != diffScreen || m.review.Basis.SourceHeadSHA != "newer-source" || m.errMessage != "" || m.infoMessage != "retained guidance" || m.diffOffset != offset {
					t.Fatal("stale response replaced newer report or view state")
				}
			})
		}
	}
}

func TestReviewRequestInvalidatedAcrossViews(t *testing.T) {
	for _, leave := range []string{"escape-reopen", "leave-record-reopen", "replace-record", "quit"} {
		t.Run(leave, func(t *testing.T) {
			report := constructedReview()
			pr := model.PR2{ID: report.ID}
			m := &Model{screen: detailScreen, currentPR: &pr}
			m.Update(testKey("d"))
			old := reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report}
			switch leave {
			case "escape-reopen", "leave-record-reopen":
				if leave == "escape-reopen" {
					m.Update(old)
					m.Update(testKey("R"))
					old.request = m.reviewRequest
				}
				m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				if leave == "leave-record-reopen" {
					m.Update(prLoadedMsg{record: pr})
				}
				m.Update(testKey("d"))
			case "replace-record":
				m.Update(prLoadedMsg{record: pr})
			case "quit":
				m.Update(testKey("q"))
			}
			screen, review, pending := m.screen, m.review, m.reviewRequest
			m.Update(old)
			old.err = errors.New("stale view error")
			m.Update(old)
			if m.screen != screen || m.review != review || m.reviewRequest != pending || m.errMessage != "" {
				t.Fatal("response from previous view applied")
			}
			if pending != nil {
				m.Update(reviewLoadedMsg{request: pending, id: pr.ID, report: report})
				if m.screen != diffScreen {
					t.Fatal("new view could not load report")
				}
			}
		})
	}
}

func TestReviewCommandCapturesItsRequest(t *testing.T) {
	// Missing IDs exercise a real read without changing repository state.
	svc, err := app.NewService(".")
	if err != nil {
		t.Fatal(err)
	}
	pr := model.PR2{ID: "missing-record"}
	m := &Model{svc: svc, screen: detailScreen, currentPR: &pr}
	_, first := m.Update(testKey("d"))
	request := m.reviewRequest
	_, second := m.Update(testKey("d"))
	latest := m.reviewRequest
	for _, tc := range []struct {
		cmd  tea.Cmd
		want *reviewRequest
	}{{second, latest}, {first, request}} {
		response := tc.cmd().(reviewLoadedMsg)
		if response.request != tc.want || response.id != pr.ID || response.err == nil {
			t.Fatal("command did not capture request identity or service error")
		}
	}
}

func TestReviewResponseRequiresMatchingViewAndRecord(t *testing.T) {
	for _, change := range []string{"list", "other-record", "no-record"} {
		t.Run(change, func(t *testing.T) {
			report := constructedReview()
			pr := model.PR2{ID: report.ID}
			m := &Model{screen: detailScreen, currentPR: &pr}
			m.Update(testKey("d"))
			response := reviewLoadedMsg{request: m.reviewRequest, id: pr.ID, report: report}
			switch change {
			case "list":
				m.screen = listScreen
			case "other-record":
				m.currentPR = &model.PR2{ID: "other-record"}
			case "no-record":
				m.currentPR = nil
			}
			screen := m.screen
			m.Update(response)
			if m.screen != screen || m.review != nil {
				t.Fatal("response applied outside its view or record")
			}
		})
	}
}
