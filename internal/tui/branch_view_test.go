package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/wyrd-company/gitpr/internal/app"
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

func TestLegacyOnlyMainListOutputRemainsByteIdentical(t *testing.T) {
	legacy := model.PR{ID: "01LEGACYTUILIST00000000000", SourceBranch: "legacy-topic", Title: "Legacy title", Status: model.StatusOpen}
	got := ansi.Strip((&Model{width: 100, height: 30, openRecords: []model.Record{legacy}}).renderList())
	want := "Open PRs\n\n> 01LEGACYTUIL  legacy-topic       Legacy title\n\nKeys: j/k move  enter open  q quit"
	if got != want {
		t.Fatalf("legacy list:\n%q\nwant:\n%q", got, want)
	}
}

func TestTUILoaderIncludesRetainedClosedBranchRecords(t *testing.T) {
	dir := t.TempDir()
	tuiGit(t, dir, "init", "-b", "main")
	tuiGit(t, dir, "config", "user.name", "test user")
	tuiGit(t, dir, "config", "user.email", "test@example.test")
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tuiGit(t, dir, "add", "sample.txt")
	tuiGit(t, dir, "commit", "-m", "base")
	tuiGit(t, dir, "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("base\nchange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tuiGit(t, dir, "add", "sample.txt")
	tuiGit(t, dir, "commit", "-m", "change")
	svc, err := app.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	pr, _, err := svc.CreatePR(context.Background(), app.CreatePRRequest{Title: "Closed record", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ClosePR(pr.ID, app.ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
		t.Fatal(err)
	}
	msg := (&Model{svc: svc}).loadOpenPRsCmd()().(listLoadedMsg)
	if msg.err != nil || len(msg.records) != 1 {
		t.Fatalf("loaded records=%#v err=%v", msg.records, msg.err)
	}
	loaded, ok := msg.records[0].(model.PR2)
	if !ok || loaded.State != model.PRStateClosed || loaded.Closure == nil || loaded.Closure.Reason != model.ClosureAbandoned {
		t.Fatalf("loaded=%#v", msg.records[0])
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

func tuiGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), output, err)
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
