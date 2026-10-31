//go:build test

// relationships:
//
//	implements: branch-based-pull-requests
package app

import (
	"context"
	"strings"
	"testing"
)

// Hold the verdict after service validation but before the ref transaction,
// replace the annotation without changing its commit, then settle the writer.
func TestTagBaseTransactionRefusesConcurrentReplacement(t *testing.T) {
	for _, verdict := range []string{"approve", "reject"} {
		t.Run(verdict, func(t *testing.T) {
			dir, s := newBranchService(t)
			ctx := context.Background()
			testGit(t, dir, "tag", "-a", "release", "refs/heads/main", "-m", "original")
			pr, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Review", Worktree: dir, BaseBranch: "release"})
			if err != nil {
				t.Fatal(err)
			}
			report, err := s.ReviewPR(ctx, pr.ID)
			if err != nil {
				t.Fatal(err)
			}
			heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
			s.store.SetBeforeSaveHook(func() {
				s.store.SetBeforeSaveHook(nil)
				testGit(t, dir, "tag", "-f", "-a", "release", "refs/heads/main", "-m", "replacement")
			})
			before := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
			if verdict == "approve" {
				_, _, err = s.ApprovePR(ctx, pr.ID, heads)
			} else {
				_, _, err = s.RejectPR(ctx, pr.ID, &heads)
			}
			if err == nil || !strings.Contains(err.Error(), "base tag") {
				t.Fatalf("race error: %v", err)
			}
			if after := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr"); after != before {
				t.Fatal("race appended verdict or pins")
			}
			loaded, _, err := s.LoadRecord(pr.ID)
			if err != nil || len(loaded.Events) != 0 {
				t.Fatalf("persisted events: %#v %v", loaded.Events, err)
			}
		})
	}
}

func TestTagBaseCreationVerifiesOriginalObject(t *testing.T) {
	dir, s := newBranchService(t)
	testGit(t, dir, "tag", "release", "refs/heads/main")
	s.store.SetBeforeSaveHook(func() { s.store.SetBeforeSaveHook(nil); testGit(t, dir, "tag", "-f", "release", "refs/heads/feature") })
	if _, _, err := s.CreatePR(context.Background(), CreatePRRequest{Title: "Review", Worktree: dir, BaseBranch: "release"}); err == nil {
		t.Fatal("created PR after tag changed")
	}
	if refs := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr"); refs != "" {
		t.Fatalf("creation wrote refs: %s", refs)
	}
}
