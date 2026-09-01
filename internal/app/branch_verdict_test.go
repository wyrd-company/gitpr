package app

import (
	"context"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestApprovePRAppendsExactEventsPinsAndReleasesMatchingAnchors(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Verdict", Worktree: repoPath})
	basis, _ := service.ReviewPR(context.Background(), pr.ID)
	baseBefore := testGit(t, repoPath, "rev-parse", "refs/heads/main")
	stored, version, _ := service.store.LoadPR2(pr.ID)
	stored.Threads = []model.Thread{{ID: "01ANCHORVERDICT00000000000", Kind: model.ThreadAnchored, Status: model.ThreadOpen, Anchor: &model.ThreadAnchor{SourceHeadSHA: basis.Basis.SourceHeadSHA, BaseHeadSHA: basis.Basis.BaseHeadSHA, File: "sample.txt", Side: model.DiffSideSource, LineStart: 2, LineEnd: 2}}}
	if _, err := service.store.SavePR2(stored, stored.State, version); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, repoPath, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+pr.ID+"/anchors"); got == "" {
		t.Fatal("anchor refs were not created")
	}
	heads := ExpectedHeads{Source: basis.Basis.SourceHeadSHA, Base: basis.Basis.BaseHeadSHA}
	first, _, err := service.ApprovePR(context.Background(), pr.ID, heads)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.ApprovePR(context.Background(), pr.ID, heads)
	if err != nil {
		t.Fatal(err)
	}
	if second.State != model.PRStateOpen || len(second.Events) != 2 {
		t.Fatalf("approved PR = %#v", second)
	}
	for i, event := range second.Events {
		if event.SourceHeadSHA != heads.Source || event.BaseHeadSHA != heads.Base || event.MergeBaseSHA != heads.Base || event.Verdict != model.VerdictAccepted || event.Timestamp.IsZero() {
			t.Errorf("event %d = %#v", i, event)
		}
	}
	if first.Events[0].PredecessorEventID != "" || second.Events[1].PredecessorEventID != second.Events[0].ID {
		t.Fatalf("event lineage = %#v", second.Events)
	}
	for _, suffix := range []string{"head", "base"} {
		assertAppRefExists(t, repoPath, "refs/gitpr/pr/"+pr.ID+"/events/"+second.Events[0].ID+"/"+suffix)
		assertAppRefExists(t, repoPath, "refs/gitpr/pr/"+pr.ID+"/events/"+second.Events[1].ID+"/"+suffix)
	}
	if got := testGit(t, repoPath, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+pr.ID+"/anchors"); got != "" {
		t.Fatalf("matching anchor refs remain: %s", got)
	}
	if got := testGit(t, repoPath, "rev-parse", "refs/heads/main"); got != baseBefore {
		t.Fatalf("approve moved base from %s to %s", baseBefore, got)
	}
}

func TestApprovePRRefusesSideSpecificDriftWithoutMutation(t *testing.T) {
	tests := []struct {
		name string
		move func(*testing.T, string)
		want []string
	}{
		{name: "source moved", move: func(t *testing.T, dir string) {
			writeTestFile(t, dir, "later.txt", "later\n")
			testGit(t, dir, "add", "later.txt")
			testGit(t, dir, "commit", "-m", "later")
		}, want: []string{"source", "expected head", "live head"}},
		{name: "base moved", move: func(t *testing.T, dir string) {
			testGit(t, dir, "checkout", "main")
			writeTestFile(t, dir, "base.txt", "later\n")
			testGit(t, dir, "add", "base.txt")
			testGit(t, dir, "commit", "-m", "base later")
		}, want: []string{"base", "expected head", "live head"}},
		{name: "source deleted", move: func(t *testing.T, dir string) {
			testGit(t, dir, "checkout", "main")
			testGit(t, dir, "branch", "-D", "feature")
		}, want: []string{"source", "expected head", "deleted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, service := newBranchService(t)
			pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Drift", Worktree: dir})
			report, _ := service.ReviewPR(context.Background(), pr.ID)
			beforeRefs := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
			tt.move(t, dir)
			_, _, err := service.ApprovePR(context.Background(), pr.ID, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA})
			if err == nil {
				t.Fatal("ApprovePR() error = nil")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err, want)
				}
			}
			loaded, _, _ := service.store.LoadPR2(pr.ID)
			if len(loaded.Events) != 0 {
				t.Fatalf("drift appended events: %#v", loaded.Events)
			}
			afterRefs := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
			if afterRefs != beforeRefs {
				t.Fatalf("drift mutated gitpr refs\nbefore: %s\nafter: %s", beforeRefs, afterRefs)
			}
		})
	}
}

func TestRejectPR2RecordsExpectedPairDespiteLiveDrift(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Reject", Worktree: dir})
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "branch", "-D", "feature")
	record, _, err := service.RejectRecord(context.Background(), pr.ID, &heads)
	if err != nil {
		t.Fatal(err)
	}
	got := record.(model.PR2)
	if len(got.Events) != 1 || got.Events[0].Verdict != model.VerdictRejected || got.Events[0].SourceHeadSHA != heads.Source || got.Events[0].BaseHeadSHA != heads.Base {
		t.Fatalf("rejected event = %#v", got.Events)
	}
}

func TestRejectPR2RefusesUnresolvableExpectedCommit(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Reject", Worktree: dir})
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: strings.Repeat("f", 40), Base: report.Basis.BaseHeadSHA}
	if _, _, err := service.RejectRecord(context.Background(), pr.ID, &heads); err == nil || !strings.Contains(err.Error(), "source") || !strings.Contains(err.Error(), "does not resolve to a commit") {
		t.Fatalf("reject invalid object error = %v", err)
	}
}

func TestBranchVerdictsRequireCompleteReviewedPair(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Pair", Worktree: dir})
	for _, heads := range []ExpectedHeads{{}, {Source: "only-source"}, {Base: "only-base"}} {
		if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err == nil || !strings.Contains(err.Error(), "gitpr review") {
			t.Errorf("approve heads %#v error = %v", heads, err)
		}
		if _, _, err := service.RejectRecord(context.Background(), pr.ID, &heads); err == nil || !strings.Contains(err.Error(), "gitpr review") {
			t.Errorf("reject heads %#v error = %v", heads, err)
		}
	}
	if _, _, err := service.RejectRecord(context.Background(), pr.ID, nil); err == nil || !strings.Contains(err.Error(), "gitpr review") {
		t.Errorf("reject nil heads error = %v", err)
	}
}

func TestBranchVerdictsRefuseTerminalRecords(t *testing.T) {
	for _, state := range []model.PRState{model.PRStateMerged, model.PRStateClosed} {
		t.Run(string(state), func(t *testing.T) {
			dir, service := newBranchService(t)
			pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Terminal", Worktree: dir})
			report, _ := service.ReviewPR(context.Background(), pr.ID)
			heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
			stored, version, _ := service.store.LoadPR2(pr.ID)
			stored.State = state
			if state == model.PRStateClosed {
				stored.Closure = &model.Closure{Reason: model.ClosureAbandoned}
			}
			if _, err := service.store.SavePR2(stored, model.PRStateOpen, version); err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err == nil || !strings.Contains(err.Error(), "only while open") {
				t.Errorf("approve terminal error = %v", err)
			}
			if _, _, err := service.RejectRecord(context.Background(), pr.ID, &heads); err == nil || !strings.Contains(err.Error(), "only while open") {
				t.Errorf("reject terminal error = %v", err)
			}
		})
	}
}

func TestApprovePRRefusesLegacyRecordWithNewVerbDiagnostic(t *testing.T) {
	service, legacy := newTestPR(t)
	if _, _, err := service.ApprovePR(context.Background(), legacy.ID, ExpectedHeads{Source: legacy.SourceHeadSHA, Base: legacy.BaseHeadSHA}); err == nil || !strings.Contains(err.Error(), "branch-based") || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("legacy approve error = %v", err)
	}
}

func TestRejectRecordPreservesLegacyRejectBehaviorWithoutReviewedHeads(t *testing.T) {
	service, legacy := newTestPR(t)
	record, _, err := service.RejectRecord(context.Background(), legacy.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := record.(model.PR)
	if !ok || got.Status != model.StatusRejected {
		t.Fatalf("legacy rejected record = %#v", record)
	}
}
