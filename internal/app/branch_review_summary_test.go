// relationships:
//
//	implements: branch-review
package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"gopkg.in/yaml.v3"
)

func TestReviewSummaryCountsMetadataWithoutPatchesOrWrites(t *testing.T) {
	dir, svc := newBranchService(t)
	ctx := context.Background()
	for _, path := range []string{"removed.txt", "unchanged.txt", "odd\n path.txt", "binary.dat"} {
		writeTestFile(t, dir, path, "original\x00content\n")
	}
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "initial files")
	pr, _, err := svc.CreatePR(ctx, CreatePRRequest{Title: "Summary", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.LatestEvent != nil || initial.InterdiffSummary != nil || len(initial.Diff) != 0 {
		t.Fatalf("initial = %#v", initial)
	}
	heads := ExpectedHeads{Source: initial.Basis.SourceHeadSHA, Base: initial.Basis.BaseHeadSHA}
	if _, _, err := svc.ApprovePR(ctx, pr.ID, heads); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CommentPR2(ctx, pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 2, Text: "large comment body"}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, dir, "sample.txt", "base\nchanged\n")
	writeTestFile(t, dir, "odd\n path.txt", "modified\x00content\n")
	writeTestFile(t, dir, "binary.dat", "modified\x00binary\n")
	testGit(t, dir, "rm", "removed.txt")
	writeTestFile(t, dir, "added.txt", "added\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "move source")
	before, version, err := svc.store.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	refs := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)")
	status := testGit(t, dir, "status", "--porcelain")
	tracePath := filepath.Join(t.TempDir(), "git.trace")
	t.Setenv("GIT_TRACE", tracePath)
	summary, err := svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(trace), " diff-tree ") || strings.Contains(string(trace), " diff ") {
		t.Fatalf("expected raw metadata only:\n%s", trace)
	}
	want := &InterdiffSummary{PreviousFiles: 5, CurrentFiles: 5, Added: 1, Removed: 1, Changed: 3, Unchanged: 1}
	if !reflect.DeepEqual(summary.InterdiffSummary, want) {
		t.Fatalf("counts = %#v, want %#v", summary.InterdiffSummary, want)
	}
	if summary.LatestEvent == nil || summary.LatestEvent.Verdict != model.VerdictAccepted || summary.Basis.SourceHeadSHA == heads.Source || summary.Basis.BaseContained == nil || !*summary.Basis.BaseContained {
		t.Fatalf("state = %#v", summary)
	}
	full, err := svc.ReviewPR(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full.Basis, summary.Basis) || len(summary.Threads) != 1 || !summary.Threads[0].Outdated || len(summary.Threads[0].Comments) != 0 || !reflect.DeepEqual(full.Threads[0].Anchor, summary.Threads[0].Anchor) {
		t.Fatalf("projected state = %#v", summary)
	}
	encoded, err := yaml.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\ndiff:", "\ninterdiff:", "patch:", "hunks:", "large comment body", "added.txt"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("summary contains %q:\n%s", forbidden, encoded)
		}
	}
	after, afterVersion, err := svc.store.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if version != afterVersion || !reflect.DeepEqual(before, after) || refs != testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)") || status != testGit(t, dir, "status", "--porcelain") {
		t.Fatal("review changed persisted state")
	}
}

func TestReviewSummaryTracksDivergedAndMissingBranches(t *testing.T) {
	dir, svc := newBranchService(t)
	ctx := context.Background()
	pr, _, err := svc.CreatePR(ctx, CreatePRRequest{Title: "Basis", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "checkout", "main")
	writeTestFile(t, dir, "base.txt", "base\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "base movement")
	summary, err := svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil || summary.Basis.BaseContained == nil || *summary.Basis.BaseContained {
		t.Fatalf("diverged = %#v, %v", summary, err)
	}
	testGit(t, dir, "branch", "-D", "feature")
	summary, err = svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil || !summary.Basis.SourceBranchMissing || summary.Basis.BaseHeadSHA == "" {
		t.Fatalf("missing source = %#v, %v", summary, err)
	}
	testGit(t, dir, "checkout", "-b", "other")
	testGit(t, dir, "branch", "-D", "main")
	summary, err = svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil || !summary.Basis.BaseBranchMissing {
		t.Fatalf("missing base = %#v, %v", summary, err)
	}
}

func TestReviewSummaryCountsRenamesAndModeChanges(t *testing.T) {
	dir, svc := newBranchService(t)
	ctx := context.Background()
	writeTestFile(t, dir, "mode.txt", "content\n")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "mode fixture")
	pr, _, err := svc.CreatePR(ctx, CreatePRRequest{Title: "Metadata changes", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ApprovePR(ctx, pr.ID, ExpectedHeads{Source: initial.Basis.SourceHeadSHA, Base: initial.Basis.BaseHeadSHA}); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "mv", "sample.txt", "renamed.txt")
	testGit(t, dir, "update-index", "--chmod=+x", "mode.txt")
	testGit(t, dir, "commit", "-m", "rename and mode")
	summary, err := svc.ReviewPRSummary(ctx, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := &InterdiffSummary{PreviousFiles: 2, CurrentFiles: 3, Added: 1, Changed: 2}
	// sample.txt now records deletion from the base; renamed.txt is an addition.
	// Both are new/changed diff entries compared with the previous review.
	if !reflect.DeepEqual(summary.InterdiffSummary, want) {
		t.Fatalf("counts = %#v, want %#v", summary.InterdiffSummary, want)
	}
}
