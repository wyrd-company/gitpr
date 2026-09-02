package app

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func TestMergePR2AtomicallyAdvancesBaseAndRecord(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	baseBefore := event.BaseHeadSHA
	merged, ref, err := service.MergePR(context.Background(), pr.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if merged.State != model.PRStateMerged || merged.MergedAt == nil || merged.MergedEventID != event.ID || ref == "" {
		t.Fatalf("merged record = %#v, ref=%q", merged, ref)
	}
	if got := testGit(t, dir, "rev-parse", "refs/heads/main"); got != event.SourceHeadSHA || got == baseBefore {
		t.Fatalf("base = %s, want %s", got, event.SourceHeadSHA)
	}
	assertAppRefExists(t, dir, "refs/gitpr/index/merged/"+pr.ID)
	if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/index/open/"+pr.ID); got != "" {
		t.Fatalf("open index remains: %s", got)
	}
	if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/openpair"); got != "" {
		t.Fatalf("open-pair ref remains: %s", got)
	}
}

func TestMergePR2LatestRejectedEventBlocksOlderAcceptance(t *testing.T) {
	_, service, pr, event := newAcceptedBranchPR(t)
	heads := ExpectedHeads{Source: event.SourceHeadSHA, Base: event.BaseHeadSHA}
	if _, _, err := service.RejectPR(context.Background(), pr.ID, &heads); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "latest") || !strings.Contains(err.Error(), "rejected") || !strings.Contains(err.Error(), "new accepted event") {
		t.Fatalf("rejected-latest error = %v", err)
	}
}

func TestMergePR2RefusesPRWithoutReviewEvent(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "No verdict", Worktree: dir})
	if _, _, err := service.MergePR(context.Background(), pr.ID, false); err == nil || !strings.Contains(err.Error(), "accepted event") {
		t.Fatalf("no-event error = %v", err)
	}
}

func TestMergePRReturnsNoRefOnPreCommitRefusal(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Refusal", Worktree: dir})
	record, ref, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil || record.ID != "" || ref != "" {
		t.Fatalf("refusal result = %#v, ref=%q, err=%v", record, ref, err)
	}
}

func TestMergePR2RefusesTerminalRecord(t *testing.T) {
	_, service, pr, _ := newAcceptedBranchPR(t)
	stored, version, _ := service.store.LoadPR(pr.ID)
	stored.State = model.PRStateClosed
	stored.Closure = &model.Closure{Reason: model.ClosureAbandoned}
	if _, err := service.store.SavePR2(stored, model.PRStateOpen, version); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.MergePR(context.Background(), pr.ID, false); err == nil || !strings.Contains(err.Error(), "only open") {
		t.Fatalf("terminal merge error = %v", err)
	}
}

func TestMergePR2RefusesSideSpecificLiveHeadFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		wants  []string
	}{
		{name: "source drift", mutate: func(t *testing.T, dir string) {
			writeTestFile(t, dir, "later.txt", "later\n")
			testGit(t, dir, "add", "later.txt")
			testGit(t, dir, "commit", "-m", "source later")
		}, wants: []string{"source", "recorded head", "live head"}},
		{name: "source deleted", mutate: func(t *testing.T, dir string) {
			testGit(t, dir, "checkout", "main")
			testGit(t, dir, "branch", "-D", "feature")
		}, wants: []string{"source", "recorded head", "deleted"}},
		{name: "base drift", mutate: func(t *testing.T, dir string) {
			testGit(t, dir, "checkout", "main")
			writeTestFile(t, dir, "base.txt", "base later\n")
			testGit(t, dir, "add", "base.txt")
			testGit(t, dir, "commit", "-m", "base later")
		}, wants: []string{"base", "recorded head", "live head"}},
		{name: "base deleted", mutate: func(t *testing.T, dir string) {
			testGit(t, dir, "branch", "-D", "main")
		}, wants: []string{"base", "recorded head", "deleted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, service, pr, _ := newAcceptedBranchPR(t)
			tt.mutate(t, dir)
			_, _, err := service.MergePR(context.Background(), pr.ID, false)
			if err == nil {
				t.Fatal("merge error = nil")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err, want)
				}
			}
			loaded, _, _ := service.store.LoadPR(pr.ID)
			if loaded.State != model.PRStateOpen {
				t.Fatalf("state = %s", loaded.State)
			}
		})
	}
}

func TestMergePR2RefusesDivergedAndEqualReviewPairs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*testing.T, string)
	}{
		{name: "diverged", arrange: func(t *testing.T, dir string) {
			testGit(t, dir, "checkout", "main")
			writeTestFile(t, dir, "base.txt", "base\n")
			testGit(t, dir, "add", "base.txt")
			testGit(t, dir, "commit", "-m", "diverge base")
			testGit(t, dir, "checkout", "feature")
		}},
		{name: "equal", arrange: func(t *testing.T, dir string) { testGit(t, dir, "reset", "--hard", "refs/heads/main") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, service := newBranchService(t)
			pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Ancestry", Worktree: dir})
			tc.arrange(t, dir)
			report, err := service.ReviewPR(context.Background(), pr.ID)
			if err != nil {
				t.Fatal(err)
			}
			heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
			if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err != nil {
				t.Fatal(err)
			}
			_, _, err = service.MergePR(context.Background(), pr.ID, false)
			if err == nil || !strings.Contains(err.Error(), "recorded") || (!strings.Contains(err.Error(), "fast-forward") && !strings.Contains(err.Error(), "strict")) {
				t.Fatalf("ancestry error = %v", err)
			}
		})
	}
}

func TestMergePR2RefreshesSingleCleanBaseWorktree(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	basePath := filepath.Join(t.TempDir(), "base")
	testGit(t, dir, "worktree", "add", basePath, "main")
	if _, _, err := service.MergePR(context.Background(), pr.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, basePath, "rev-parse", "HEAD"); got != event.SourceHeadSHA {
		t.Fatalf("base worktree HEAD = %s", got)
	}
	if got := testGit(t, basePath, "status", "--porcelain"); got != "" {
		t.Fatalf("base worktree status = %q", got)
	}
}

func TestMergePR2RefusesMultipleOrDirtyBaseWorktrees(t *testing.T) {
	t.Run("multiple", func(t *testing.T) {
		dir, service, pr, _ := newAcceptedBranchPR(t)
		first := filepath.Join(t.TempDir(), "base-one")
		second := filepath.Join(t.TempDir(), "base-two")
		testGit(t, dir, "worktree", "add", first, "main")
		testGit(t, dir, "worktree", "add", "--force", second, "main")
		if _, _, err := service.MergePR(context.Background(), pr.ID, false); err == nil || !strings.Contains(err.Error(), "multiple worktrees") {
			t.Fatalf("multiple error = %v", err)
		}
	})
	t.Run("dirty", func(t *testing.T) {
		dir, service, pr, _ := newAcceptedBranchPR(t)
		basePath := filepath.Join(t.TempDir(), "base")
		testGit(t, dir, "worktree", "add", basePath, "main")
		writeTestFile(t, basePath, "dirty.txt", "dirty\n")
		if _, _, err := service.MergePR(context.Background(), pr.ID, false); err == nil || !strings.Contains(err.Error(), "dirty") || !strings.Contains(err.Error(), basePath) {
			t.Fatalf("dirty error = %v", err)
		}
	})
}

func TestMergePR2ExternalBaseLeaseIsAtomicAndRetryable(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	cmd := exec.Command("git", "-C", dir, "update-ref", "--stdin")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if _, err := stdin.Write([]byte("start\nupdate refs/heads/main " + event.SourceHeadSHA + " " + event.BaseHeadSHA + "\nprepare\n")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"start: ok", "prepare: ok"} {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != want {
			t.Fatalf("lease response = %q, %v; want %q", line, err, want)
		}
	}
	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil || !errors.Is(err, store.ErrMergeConflict) {
		t.Fatalf("leased merge error = %v", err)
	}
	loaded, _, _ := service.store.LoadPR(pr.ID)
	if loaded.State != model.PRStateOpen || testGit(t, dir, "rev-parse", "refs/heads/main") != event.BaseHeadSHA {
		t.Fatalf("lease failure left partial merge: %#v", loaded)
	}
	if _, err := stdin.Write([]byte("abort\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.MergePR(context.Background(), pr.ID, false); err != nil {
		t.Fatalf("retry after lease: %v", err)
	}
}

func TestMergePR2CleanupRemovesRecordedSourceWorktreeAndBranch(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "-b", "main")
	testGit(t, root, "config", "user.name", "gitpr tests")
	testGit(t, root, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, root, "sample.txt", "base\n")
	testGit(t, root, "add", "sample.txt")
	testGit(t, root, "commit", "-m", "base")
	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, root, "worktree", "add", "-b", "feature", featurePath, "main")
	writeTestFile(t, featurePath, "sample.txt", "base\nfeature\n")
	testGit(t, featurePath, "add", "sample.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	service, _ := NewService(featurePath)
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Cleanup", Worktree: featurePath, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.MergePR(context.Background(), pr.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Fatalf("source worktree remains: %v", err)
	}
	if got := testGit(t, root, "branch", "--list", "feature"); got != "" {
		t.Fatalf("source branch remains: %s", got)
	}
}

func TestMergePR2CleanupFailureReportsCompletedMerge(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	stored, version, _ := service.store.LoadPR(pr.ID)
	stored.SourceWorktreePath = filepath.Join(t.TempDir(), "missing-source")
	if _, err := service.store.SavePR2(stored, stored.State, version); err != nil {
		t.Fatal(err)
	}
	merged, ref, err := service.MergePR(context.Background(), pr.ID, true)
	if err == nil || merged.State != model.PRStateMerged || ref == "" || !strings.Contains(err.Error(), "merge succeeded") || !strings.Contains(err.Error(), "cleanup failed") || !strings.Contains(err.Error(), stored.SourceWorktreePath) {
		t.Fatalf("cleanup failure result = %#v, ref=%q, err=%v", merged, ref, err)
	}
	if got := testGit(t, dir, "rev-parse", "refs/heads/main"); got != event.SourceHeadSHA {
		t.Fatalf("base = %s", got)
	}
}

func newAcceptedBranchPR(t *testing.T) (string, *Service, model.PR2, model.ReviewEvent) {
	t.Helper()
	dir, service := newBranchService(t)
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Merge", Worktree: dir})
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	approved, _, err := service.ApprovePR(context.Background(), pr.ID, heads)
	if err != nil {
		t.Fatal(err)
	}
	return dir, service, approved, approved.Events[len(approved.Events)-1]
}
