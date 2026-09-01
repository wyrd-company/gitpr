package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestCreatePRWritesOpenSchema2BranchRecordWithoutSnapshotState(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Branch record", Description: "live review", Worktree: repoPath, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Schema != 2 || pr.SourceBranch != "feature" || pr.BaseBranch != "main" || pr.RepositoryRoot != repoPath || pr.SourceWorktreePath != repoPath || pr.State != model.PRStateOpen {
		t.Fatalf("created PR = %#v", pr)
	}
	if len(pr.Events) != 0 || len(pr.Threads) != 0 {
		t.Fatalf("create stored review state: events=%d threads=%d", len(pr.Events), len(pr.Threads))
	}
	loaded, _, err := service.store.LoadPR2(pr.ID)
	if err != nil || !reflect.DeepEqual(loaded, pr) {
		t.Fatalf("round trip = %#v, %v", loaded, err)
	}
	assertAppRefExists(t, repoPath, "refs/gitpr/index/open/"+pr.ID)
	if got := testGit(t, repoPath, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+pr.ID+"/events", "refs/gitpr/pr/"+pr.ID+"/anchors"); got != "" {
		t.Fatalf("create wrote event/anchor refs: %s", got)
	}
}

func TestCreatePRRefusesDuplicateOpenPairButAllowsTerminalPredecessor(t *testing.T) {
	repoPath, service := newBranchService(t)
	first, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "First", Worktree: repoPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Duplicate", Worktree: repoPath}); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("duplicate create error = %v", err)
	}
	stored, version, _ := service.store.LoadPR2(first.ID)
	stored.State = model.PRStateClosed
	stored.Closure = &model.Closure{Reason: model.ClosureAbandoned}
	if _, err := service.store.SavePR2(stored, model.PRStateOpen, version); err != nil {
		t.Fatal(err)
	}
	second, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "After close", Worktree: repoPath})
	if err != nil {
		t.Fatalf("create after close: %v", err)
	}
	stored, version, _ = service.store.LoadPR2(second.ID)
	stored.State = model.PRStateMerged
	if _, err := service.store.SavePR2(stored, model.PRStateOpen, version); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "After merge", Worktree: repoPath}); err != nil {
		t.Fatalf("create after merge: %v", err)
	}
}

func TestReviewPRReportsLiveBasisDiffContainmentAndMachinePair(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Review", Worktree: repoPath})
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Basis.SourceHeadSHA != testGit(t, repoPath, "rev-parse", "refs/heads/feature") || report.Basis.BaseHeadSHA != testGit(t, repoPath, "rev-parse", "refs/heads/main") {
		t.Fatalf("basis pair = %#v", report.Basis)
	}
	if report.Basis.BaseContained == nil || !*report.Basis.BaseContained || report.Basis.MergeBaseSHA != report.Basis.BaseHeadSHA {
		t.Fatalf("containment basis = %#v", report.Basis)
	}
	if len(report.Diff) != 1 || report.Diff[0].NewPath != "sample.txt" || !strings.Contains(report.Diff[0].Patch, "+feature") {
		t.Fatalf("review diff = %#v", report.Diff)
	}
}

func TestReviewPRReflectsMovedAndDeletedSourceAndDivergedBase(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Movement", Worktree: repoPath})
	writeTestFile(t, repoPath, "later.txt", "later\n")
	testGit(t, repoPath, "add", "later.txt")
	testGit(t, repoPath, "commit", "-m", "later")
	moved := testGit(t, repoPath, "rev-parse", "HEAD")
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil || report.Basis.SourceHeadSHA != moved {
		t.Fatalf("moved basis = %#v, %v", report.Basis, err)
	}

	testGit(t, repoPath, "checkout", "main")
	writeTestFile(t, repoPath, "base-only.txt", "base\n")
	testGit(t, repoPath, "add", "base-only.txt")
	testGit(t, repoPath, "commit", "-m", "base movement")
	report, err = service.ReviewPR(context.Background(), pr.ID)
	if err != nil || report.Basis.BaseContained == nil || *report.Basis.BaseContained {
		t.Fatalf("diverged basis = %#v, %v", report.Basis, err)
	}

	testGit(t, repoPath, "branch", "-D", "feature")
	report, err = service.ReviewPR(context.Background(), pr.ID)
	if err != nil || !report.Basis.SourceBranchMissing || report.Basis.SourceHeadSHA != "" || report.Basis.BaseHeadSHA == "" {
		t.Fatalf("deleted-source basis = %#v, %v", report.Basis, err)
	}
}

func TestReviewPRReportsLatestEventInterdiffWithoutPersisting(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Interdiff", Worktree: repoPath})
	initial, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, version, _ := service.store.LoadPR2(pr.ID)
	stored.Events = []model.ReviewEvent{{ID: "01REVIEWBASISEVENT000000000", SourceHeadSHA: initial.Basis.SourceHeadSHA, BaseHeadSHA: initial.Basis.BaseHeadSHA, MergeBaseSHA: initial.Basis.MergeBaseSHA, Verdict: model.VerdictRejected, Timestamp: time.Now().UTC()}}
	if _, err := service.store.SavePR2(stored, stored.State, version); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, repoPath, "later.txt", "later\n")
	testGit(t, repoPath, "add", "later.txt")
	testGit(t, repoPath, "commit", "-m", "later")
	_, metaBefore, _ := service.store.LoadPR2(pr.ID)
	refsBefore := testGit(t, repoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, metaAfter, _ := service.store.LoadPR2(pr.ID)
	refsAfter := testGit(t, repoPath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
	if metaAfter != metaBefore || refsAfter != refsBefore {
		t.Fatalf("review persisted state\nrefs before: %s\nrefs after: %s", refsBefore, refsAfter)
	}
	if report.LatestEvent == nil || report.LatestEvent.ID != stored.Events[0].ID || report.InterdiffStyle == "" || len(report.Interdiff) != 1 || report.Interdiff[0].Path != "later.txt" || report.Interdiff[0].Change != "added-to-diff" {
		t.Fatalf("interdiff report = %#v", report)
	}
}

func TestBranchBasedCommentsRefuseWithPendingSurface(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Comments", Worktree: repoPath})
	_, err := service.AddComment(pr.ID, model.Comment{Comment: "not yet"})
	if err == nil || !strings.Contains(err.Error(), "branch-based") || !strings.Contains(err.Error(), "increment 7") {
		t.Fatalf("comment refusal = %v", err)
	}
}

func newBranchService(t *testing.T) (string, *Service) {
	t.Helper()
	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "sample.txt", "base\n")
	testGit(t, repoPath, "add", "sample.txt")
	testGit(t, repoPath, "commit", "-m", "base")
	testGit(t, repoPath, "checkout", "-b", "feature")
	writeTestFile(t, repoPath, "sample.txt", "base\nfeature\n")
	testGit(t, repoPath, "add", "sample.txt")
	testGit(t, repoPath, "commit", "-m", "feature")
	service, err := NewService(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	return repoPath, service
}

func assertAppRefExists(t *testing.T, repoPath, ref string) {
	t.Helper()
	if got := testGit(t, repoPath, "rev-parse", "--verify", ref); got == "" {
		t.Fatalf("missing ref %s", ref)
	}
}
