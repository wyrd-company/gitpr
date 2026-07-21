package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBranchFastForwardsBase(t *testing.T) {
	repoPath := newTestRepo(t)
	baseSHA := testGit(t, repoPath, "rev-parse", "main")

	testGit(t, repoPath, "checkout", "-b", "feature")
	writeTestFile(t, repoPath, "feature.txt", "feature\n")
	testGit(t, repoPath, "add", "feature.txt")
	testGit(t, repoPath, "commit", "-m", "feature")
	featureSHA := testGit(t, repoPath, "rev-parse", "HEAD")
	testGit(t, repoPath, "checkout", "main")

	repo, err := Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeBranch(context.Background(), "main", featureSHA); err != nil {
		t.Fatal(err)
	}

	if got := testGit(t, repoPath, "rev-parse", "main"); got != featureSHA {
		t.Fatalf("main = %s, want feature SHA %s", got, featureSHA)
	}
	if got := testGit(t, repoPath, "rev-list", "--parents", "-n", "1", "main"); got != featureSHA+" "+baseSHA {
		t.Fatalf("fast-forwarded commit parents = %q, want %q", got, featureSHA+" "+baseSHA)
	}
}

func TestMergeBranchSynchronizesCheckedOutBaseWorktree(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contents string
	}{
		{
			name:     "added file",
			path:     "feature.txt",
			contents: "feature\n",
		},
		{
			name:     "modified file",
			path:     "base.txt",
			contents: "modified by feature\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := newTestRepo(t)
			baseSHA := testGit(t, repoPath, "rev-parse", "main")
			featurePath := filepath.Join(t.TempDir(), "feature")
			testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
			writeTestFile(t, featurePath, tt.path, tt.contents)
			testGit(t, featurePath, "add", tt.path)
			testGit(t, featurePath, "commit", "-m", tt.name)
			featureSHA := testGit(t, featurePath, "rev-parse", "HEAD")

			repo, err := Open(featurePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.MergeBranch(context.Background(), "main", featureSHA); err != nil {
				t.Fatal(err)
			}

			if got := testGit(t, repoPath, "rev-parse", "HEAD"); got != featureSHA {
				t.Fatalf("checked-out main HEAD = %s, want feature SHA %s", got, featureSHA)
			}
			if got := readTestFile(t, repoPath, tt.path); got != tt.contents {
				t.Fatalf("checked-out main %s = %q, want %q", tt.path, got, tt.contents)
			}
			if got := testGit(t, repoPath, "status", "--porcelain"); got != "" {
				t.Fatalf("checked-out main status = %q, want clean", got)
			}
			if got := testGit(t, repoPath, "rev-list", "--parents", "-n", "1", "main"); got != featureSHA+" "+baseSHA {
				t.Fatalf("fast-forwarded commit parents = %q, want %q", got, featureSHA+" "+baseSHA)
			}
		})
	}
}

func TestMergeBranchPreservesUnrelatedChangesInCheckedOutBaseWorktree(t *testing.T) {
	repoPath := newTestRepo(t)
	writeTestFile(t, repoPath, "local.txt", "committed\n")
	testGit(t, repoPath, "add", "local.txt")
	testGit(t, repoPath, "commit", "-m", "add local file")

	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "feature.txt", "feature\n")
	testGit(t, featurePath, "add", "feature.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	featureSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	writeTestFile(t, repoPath, "local.txt", "uncommitted local change\n")
	repo, err := Open(featurePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeBranch(context.Background(), "main", featureSHA); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, repoPath, "local.txt"); got != "uncommitted local change\n" {
		t.Fatalf("unrelated local change = %q, want preserved contents", got)
	}
	if got := readTestFile(t, repoPath, "feature.txt"); got != "feature\n" {
		t.Fatalf("merged feature file = %q, want feature contents", got)
	}
}

func TestMergeBranchRefusesBeforeAdvancingBaseWhenLocalChangeConflicts(t *testing.T) {
	repoPath := newTestRepo(t)
	baseSHA := testGit(t, repoPath, "rev-parse", "main")
	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "base.txt", "feature change\n")
	testGit(t, featurePath, "add", "base.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	featureSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	writeTestFile(t, repoPath, "base.txt", "uncommitted local change\n")
	repo, err := Open(featurePath)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.MergeBranch(context.Background(), "main", featureSHA)
	if err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("MergeBranch() error = %v, want actionable local changes error", err)
	}

	if got := testGit(t, repoPath, "rev-parse", "main"); got != baseSHA {
		t.Fatalf("main changed to %s after rejected merge; want %s", got, baseSHA)
	}
	if got := readTestFile(t, repoPath, "base.txt"); got != "uncommitted local change\n" {
		t.Fatalf("conflicting local change = %q, want preserved contents", got)
	}
}

func TestMergeBranchRefusesWhenBaseIsCheckedOutInMultipleWorktrees(t *testing.T) {
	repoPath := newTestRepo(t)
	baseSHA := testGit(t, repoPath, "rev-parse", "main")
	secondBasePath := filepath.Join(t.TempDir(), "second-main")
	testGit(t, repoPath, "worktree", "add", "--force", secondBasePath, "main")

	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "feature.txt", "feature\n")
	testGit(t, featurePath, "add", "feature.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	featureSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	repo, err := Open(featurePath)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.MergeBranch(context.Background(), "main", featureSHA)
	if err == nil || !strings.Contains(err.Error(), "checked out in multiple worktrees") {
		t.Fatalf("MergeBranch() error = %v, want multiple worktrees error", err)
	}

	if got := testGit(t, repoPath, "rev-parse", "main"); got != baseSHA {
		t.Fatalf("main changed to %s after rejected merge; want %s", got, baseSHA)
	}
}

func TestMergeBranchThenCleanupRemovesSourceWorktreeAndBranch(t *testing.T) {
	repoPath := newTestRepo(t)
	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "feature.txt", "feature\n")
	testGit(t, featurePath, "add", "feature.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	featureSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	repo, err := Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeBranch(context.Background(), "main", featureSHA); err != nil {
		t.Fatal(err)
	}
	if err := repo.CleanupSourceWorktree(context.Background(), featurePath, "feature"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Fatalf("source worktree still exists or stat failed unexpectedly: %v", err)
	}
	if got := testGit(t, repoPath, "branch", "--list", "feature"); got != "" {
		t.Fatalf("source branch still exists: %q", got)
	}
}

func TestMergeBranchRejectsNonFastForward(t *testing.T) {
	repoPath := newTestRepo(t)

	testGit(t, repoPath, "checkout", "-b", "feature")
	writeTestFile(t, repoPath, "feature.txt", "feature\n")
	testGit(t, repoPath, "add", "feature.txt")
	testGit(t, repoPath, "commit", "-m", "feature")
	featureSHA := testGit(t, repoPath, "rev-parse", "HEAD")

	testGit(t, repoPath, "checkout", "main")
	writeTestFile(t, repoPath, "main.txt", "main\n")
	testGit(t, repoPath, "add", "main.txt")
	testGit(t, repoPath, "commit", "-m", "main")
	mainSHA := testGit(t, repoPath, "rev-parse", "HEAD")

	repo, err := Open(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.MergeBranch(context.Background(), "main", featureSHA)
	if err == nil {
		t.Fatalf("MergeBranch() error = %v, want non-fast-forward error", err)
	}
	if got := testGit(t, repoPath, "rev-parse", "main"); got != mainSHA {
		t.Fatalf("main changed to %s after rejected merge; want %s", got, mainSHA)
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "base.txt", "base\n")
	testGit(t, repoPath, "add", "base.txt")
	testGit(t, repoPath, "commit", "-m", "base")
	return repoPath
}

func writeTestFile(t *testing.T, repoPath, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, repoPath, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repoPath, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func testGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
