// relationships:
//
//	implements: branch-based-pull-requests
package app

import (
	"context"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestTagBaseLifecycle(t *testing.T) {
	for _, annotated := range []bool{false, true} {
		t.Run(map[bool]string{false: "lightweight", true: "annotated"}[annotated], func(t *testing.T) {
			dir, s := newBranchService(t)
			ctx := context.Background()
			args := []string{"tag", "release", "refs/heads/main"}
			if annotated {
				args = []string{"tag", "-a", "release", "refs/heads/main", "-m", "release"}
			}
			testGit(t, dir, args...)
			original := testGit(t, dir, "rev-parse", "refs/tags/release")
			pr, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Tag review", Worktree: dir, BaseBranch: "release"})
			if err != nil {
				t.Fatal(err)
			}
			if pr.BaseBranch != "refs/tags/release" || pr.BaseTag == nil || pr.BaseTag.ObjectSHA != original || pr.BaseTag.CommitSHA != testGit(t, dir, "rev-parse", "refs/heads/main") {
				t.Fatalf("identity: %#v", pr)
			}
			cfg, err := s.store.LoadConfig()
			if err != nil || cfg.DefaultBranch == pr.BaseBranch {
				t.Fatalf("tag changed branch default: %#v %v", cfg, err)
			}
			report, err := s.ReviewPR(ctx, pr.ID)
			if err != nil || len(report.Diff) == 0 || report.BaseTag == nil || report.Basis.BaseHeadSHA != pr.BaseTag.CommitSHA {
				t.Fatalf("review: %#v %v", report, err)
			}
			heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
			if _, _, err = s.CommentPR2(ctx, pr.ID, ThreadCommentRequest{PRLevel: true, Text: "Finding"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.RejectPR(ctx, pr.ID, &heads); err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.ApprovePR(ctx, pr.ID, heads); err != nil {
				t.Fatal(err)
			}
			before := testGit(t, dir, "show-ref")
			if _, _, err = s.MergePR(ctx, pr.ID, true); err == nil || !strings.Contains(err.Error(), "immutable tag") {
				t.Fatalf("merge error: %v", err)
			}
			if after := testGit(t, dir, "show-ref"); after != before {
				t.Fatal("refusal mutated refs")
			}
			closed, _, err := s.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureIntegrated, Destination: "destination", Commits: []string{heads.Source}})
			if err != nil || closed.State != model.PRStateClosed {
				t.Fatalf("close: %#v %v", closed, err)
			}
			if testGit(t, dir, "rev-parse", "refs/tags/release") != original {
				t.Fatal("tag changed")
			}
			testGit(t, dir, "tag", "-d", "release")
			testGit(t, dir, "reflog", "expire", "--expire=now", "--all")
			testGit(t, dir, "gc", "--prune=now")
			testGit(t, dir, "cat-file", "-e", original)
		})
	}
}

func TestTagBaseLossAndDriftRefuseBothVerdicts(t *testing.T) {
	for _, change := range []string{"delete", "move", "replace-annotation"} {
		for _, verdict := range []string{"approve", "reject"} {
			t.Run(change+"/"+verdict, func(t *testing.T) {
				dir, s := newBranchService(t)
				ctx := context.Background()
				testGit(t, dir, "tag", "-a", "release", "refs/heads/main", "-m", "original")
				pr, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Review", Worktree: dir, BaseBranch: "refs/tags/release"})
				if err != nil {
					t.Fatal(err)
				}
				report, err := s.ReviewPR(ctx, pr.ID)
				if err != nil {
					t.Fatal(err)
				}
				heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
				switch change {
				case "delete":
					testGit(t, dir, "tag", "-d", "release")
				case "move":
					testGit(t, dir, "tag", "-f", "release", "refs/heads/feature")
				case "replace-annotation":
					testGit(t, dir, "tag", "-f", "-a", "release", "refs/heads/main", "-m", "replacement")
				}
				before := testGit(t, dir, "show-ref")
				if verdict == "approve" {
					_, _, err = s.ApprovePR(ctx, pr.ID, heads)
				} else {
					_, _, err = s.RejectPR(ctx, pr.ID, &heads)
				}
				if err == nil || !strings.Contains(err.Error(), "base tag") {
					t.Fatalf("verdict: %v", err)
				}
				if _, err = s.ReviewPR(ctx, pr.ID); err == nil {
					t.Fatal("review accepted changed tag")
				}
				if after := testGit(t, dir, "show-ref"); after != before {
					t.Fatal("refusal mutated refs")
				}
				if _, _, err = s.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestTagBaseSelectionAndInvalidTargets(t *testing.T) {
	dir, s := newBranchService(t)
	ctx := context.Background()
	testGit(t, dir, "tag", "main", "refs/heads/feature")
	branch, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Branch", Worktree: dir, BaseBranch: "main"})
	if err != nil || branch.BaseTag != nil {
		t.Fatalf("branch precedence: %#v %v", branch, err)
	}
	tag, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Tag", Worktree: dir, BaseBranch: "refs/tags/main"})
	if err != nil || tag.BaseTag == nil {
		t.Fatalf("explicit tag: %#v %v", tag, err)
	}
	if _, _, err = s.CreatePR(ctx, CreatePRRequest{Title: "Duplicate", Worktree: dir, BaseBranch: "refs/tags/main"}); err == nil {
		t.Fatal("duplicate allowed")
	}
	for _, target := range []string{"refs/tags/missing", "refs/tags/bad\nname", "blob-tag"} {
		if target == "blob-tag" {
			blob := testGit(t, dir, "rev-parse", "HEAD:sample.txt")
			testGit(t, dir, "tag", "blob-tag", blob)
		}
		before := testGit(t, dir, "show-ref")
		if _, _, err = s.CreatePR(ctx, CreatePRRequest{Title: "Invalid", Worktree: dir, BaseBranch: target}); err == nil {
			t.Fatalf("accepted %s", target)
		}
		if after := testGit(t, dir, "show-ref"); after != before {
			t.Fatal("invalid create mutated refs")
		}
	}
	report, err := s.ReviewPR(ctx, tag.ID)
	if err != nil {
		t.Fatal(err)
	}
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: testGit(t, dir, "rev-parse", "refs/heads/main")}
	if _, _, err = s.RejectPR(ctx, tag.ID, &heads); err == nil {
		t.Fatal("accepted a base other than tag commit")
	}
}

func TestTagBaseStoreGuardsAndDeletion(t *testing.T) {
	dir, s := newBranchService(t)
	ctx := context.Background()
	testGit(t, dir, "tag", "release", "refs/heads/main")
	pr, _, err := s.CreatePR(ctx, CreatePRRequest{Title: "Review", Worktree: dir, BaseBranch: "release"})
	if err != nil {
		t.Fatal(err)
	}
	stored, version, err := s.LoadRecord(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := testGit(t, dir, "show-ref")
	changed := stored
	changed.BaseTag = nil
	if _, err = s.store.SavePR2(changed, stored.State, version); err == nil {
		t.Fatal("store allowed tag identity removal")
	}
	changed = stored
	changed.State = model.PRStateMerged
	if _, err = s.store.MergePR2(changed, version); err == nil || !strings.Contains(err.Error(), "immutable tag") {
		t.Fatalf("direct merge error: %v", err)
	}
	if after := testGit(t, dir, "show-ref"); after != before {
		t.Fatal("store refusal mutated refs")
	}
	testGit(t, dir, "tag", "-d", "release")
	reportHeads := ExpectedHeads{Source: testGit(t, dir, "rev-parse", "HEAD"), Base: stored.BaseTag.CommitSHA}
	if _, _, err = s.CommentPR2(ctx, pr.ID, ThreadCommentRequest{PRLevel: true, Text: "Disposition", Heads: &reportHeads}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.EditPR(pr.ID, EditPRRequest{Description: func() *string { v := "Updated"; return &v }()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned}); err != nil {
		t.Fatal(err)
	}
	stored, version, err = s.LoadRecord(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.DeletePR2(stored, version); err != nil {
		t.Fatal(err)
	}
	if refs := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+pr.ID); refs != "" {
		t.Fatalf("delete retained refs: %s", refs)
	}
}
