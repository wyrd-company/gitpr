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

func testGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
