package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestMergePRMergesMatchingSourceHeadAndCleansUp(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)

	if _, _, err := service.MergePR(context.Background(), pr.ID, true); err != nil {
		t.Fatal(err)
	}

	if got := testGit(t, repoPath, "rev-parse", "main"); got != pr.SourceHeadSHA {
		t.Fatalf("main = %s, want source head %s", got, pr.SourceHeadSHA)
	}
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Fatalf("source worktree still exists or stat failed unexpectedly: %v", err)
	}
	if got := testGit(t, repoPath, "branch", "--list", "feature"); got != "" {
		t.Fatalf("source branch still exists: %q", got)
	}
}

func TestMergePRRefusesMovedSourceHead(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)
	testGit(t, repoPath, "tag", "feature", pr.SourceHeadSHA)
	writeTestFile(t, featurePath, "follow-up.txt", "follow-up\n")
	testGit(t, featurePath, "add", "follow-up.txt")
	testGit(t, featurePath, "commit", "-m", "follow-up")
	currentHeadSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil {
		t.Fatal("MergePR() error = nil, want moved source head error")
	}
	for _, want := range []string{pr.SourceHeadSHA, currentHeadSHA, "reject", "create a new PR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("MergePR() error = %q, want it to contain %q", err, want)
		}
	}
	if got := testGit(t, repoPath, "rev-parse", "main"); got == pr.SourceHeadSHA {
		t.Fatalf("main advanced to reviewed source head %s after rejected merge", got)
	}
}

func TestMergePRRefusesDeletedSourceBranch(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)
	testGit(t, repoPath, "worktree", "remove", "--force", featurePath)
	testGit(t, repoPath, "branch", "-D", "feature")

	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil {
		t.Fatal("MergePR() error = nil, want deleted source branch error")
	}
	for _, want := range []string{"feature", "no longer exists", "reject", "create a new PR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("MergePR() error = %q, want it to contain %q", err, want)
		}
	}
}

func newMergeTestPR(t *testing.T) (string, string, *Service, model.PR) {
	t.Helper()
	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "base.txt", "base\n")
	testGit(t, repoPath, "add", "base.txt")
	testGit(t, repoPath, "commit", "-m", "base")

	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "feature.txt", "feature\n")
	testGit(t, featurePath, "add", "feature.txt")
	testGit(t, featurePath, "commit", "-m", "feature")

	service, err := NewService(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{
		Title:    "Guard reviewed snapshot",
		Worktree: featurePath,
	})
	if err != nil {
		t.Fatal(err)
	}

	return repoPath, featurePath, service, pr
}

func testGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, repoPath, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
