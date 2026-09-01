package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func TestClosePRPersistsEachReasonAndReleasesOpenOwnership(t *testing.T) {
	for _, reason := range []model.ClosureReason{model.ClosureIntegrated, model.ClosureSuperseded, model.ClosureAbandoned} {
		t.Run(string(reason), func(t *testing.T) {
			dir, service := newBranchService(t)
			pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Close", Worktree: dir})
			req := ClosePRRequest{Reason: reason, Note: "closure note"}
			switch reason {
			case model.ClosureIntegrated:
				req.Destination = "release"
				req.Commits = []string{testGit(t, dir, "rev-parse", "HEAD")}
				req.PatchIDs = []string{"patch-equivalent-one"}
			case model.ClosureSuperseded:
				replacement := model.PR2{Schema: 2, ID: "01REPLACEMENTCLOSE000000000", Title: "Replacement", SourceBranch: "replacement", BaseBranch: "main", RepositoryRoot: dir, State: model.PRStateOpen}
				if _, err := service.store.SavePR2(replacement, "", ""); err != nil {
					t.Fatal(err)
				}
				req.SupersededBy = replacement.ID
			}
			closed, _, err := service.ClosePR(pr.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, _ := service.store.LoadPR2(pr.ID)
			if closed.State != model.PRStateClosed || closed.ClosedAt == nil || loaded.Closure == nil || !reflect.DeepEqual(loaded.Closure, closed.Closure) {
				t.Fatalf("closed record = %#v loaded=%#v", closed, loaded)
			}
			assertAppRefExists(t, dir, "refs/gitpr/index/closed/"+pr.ID)
			if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/index/open/"+pr.ID); got != "" {
				t.Fatalf("open index remains: %s", got)
			}
			if reason != model.ClosureSuperseded {
				if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/openpair"); got != "" {
					t.Fatalf("openpair remains: %s", got)
				}
			}
		})
	}
}

func TestClosePRRefusesInvalidEvidenceBeforeMutation(t *testing.T) {
	tests := []ClosePRRequest{
		{Reason: model.ClosureIntegrated, Commits: []string{strings.Repeat("a", 40)}},
		{Reason: model.ClosureIntegrated, Destination: "release", Commits: []string{"short"}},
		{Reason: model.ClosureIntegrated, Destination: "release"},
		{Reason: model.ClosureSuperseded, SupersededBy: "01MISSINGREPLACEMENT00000000"},
	}
	for _, req := range tests {
		dir, service := newBranchService(t)
		pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Invalid close", Worktree: dir})
		before := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)")
		if _, _, err := service.ClosePR(pr.ID, req); err == nil {
			t.Fatalf("request %#v error=nil", req)
		}
		if after := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
			t.Fatalf("invalid close mutated refs")
		}
	}
}

func TestClosePRRefusesLegacyAndTerminalRecords(t *testing.T) {
	legacyService, legacy := newTestPR(t)
	if _, _, err := legacyService.ClosePR(legacy.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err == nil || !strings.Contains(err.Error(), "branch-based") {
		t.Fatalf("legacy close error=%v", err)
	}
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Terminal", Worktree: dir})
	if _, _, err := service.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err == nil || !strings.Contains(err.Error(), "closed state") {
		t.Fatalf("second close error=%v", err)
	}
}

func TestVerdictAndMergeRefuseClosedPR(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Closed gates", Worktree: dir})
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	if _, _, err := service.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err == nil || !strings.Contains(err.Error(), "only while open") {
		t.Fatalf("approve after close=%v", err)
	}
	if _, _, err := service.MergeRecord(context.Background(), pr.ID, false); err == nil || !strings.Contains(err.Error(), "only open") {
		t.Fatalf("merge after close=%v", err)
	}
}

func TestListPRsWithReasonRequiresClosedStateAndFiltersMetadata(t *testing.T) {
	dir, service := newBranchService(t)
	first, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Abandoned", Worktree: dir})
	if _, _, err := service.ClosePR(first.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
		t.Fatal(err)
	}
	second, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Integrated", Worktree: dir})
	sha := testGit(t, dir, "rev-parse", "HEAD")
	if _, _, err := service.ClosePR(second.ID, ClosePRRequest{Reason: model.ClosureIntegrated, Destination: "release", Commits: []string{sha}}); err != nil {
		t.Fatal(err)
	}
	records, err := service.ListPRsWithReason("closed", model.ClosureAbandoned)
	if err != nil || len(records) != 1 || records[0].RecordID() != first.ID {
		t.Fatalf("reason records=%#v err=%v", records, err)
	}
	if _, err := service.ListPRsWithReason("open", model.ClosureAbandoned); err == nil || !strings.Contains(err.Error(), "only with --state closed") {
		t.Fatalf("open reason error=%v", err)
	}
}

func TestDeleteRecordRemovesCompleteSchema2NamespaceInEveryState(t *testing.T) {
	for _, state := range []model.PRState{model.PRStateOpen, model.PRStateClosed, model.PRStateMerged} {
		t.Run(string(state), func(t *testing.T) {
			dir, service, pr, _ := newAcceptedBranchPR(t)
			switch state {
			case model.PRStateClosed:
				_, _, _ = service.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned})
			case model.PRStateMerged:
				_, _, _ = service.mergeBranchPR(context.Background(), pr.ID, false)
			}
			if err := service.DeleteRecord(pr.ID); err != nil {
				t.Fatal(err)
			}
			refs := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr")
			if strings.Contains(refs, pr.ID) {
				t.Fatalf("record refs remain: %s", refs)
			}
			if state == model.PRStateOpen && strings.Contains(refs, "refs/gitpr/openpair") {
				t.Fatalf("openpair remains: %s", refs)
			}
			if _, _, err := service.store.LoadPR(pr.ID); err == nil || errors.Is(err, store.ErrRecordSchema) {
				t.Fatalf("deleted load error=%v", err)
			}
		})
	}
}
