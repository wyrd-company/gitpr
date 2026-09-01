package store

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestLoadPRReturnsMetadataCommitVersion(t *testing.T) {
	st, pr := newStoreTestPR(t)

	_, version, err := st.LoadPR(pr.ID)
	if err != nil {
		t.Fatalf("LoadPR() error = %v", err)
	}
	if version == st.metaRef(pr.ID) {
		t.Fatalf("LoadPR() version = ref name %q, want commit SHA", version)
	}
	if got := storeTestGit(t, st.repo.CommonRoot, "rev-parse", st.metaRef(pr.ID)); got != version {
		t.Fatalf("LoadPR() version = %s, metadata ref = %s", version, got)
	}
}

func TestSavePRRejectsStaleMetadataVersionAndPreservesWinner(t *testing.T) {
	stA, pr := newStoreTestPR(t)
	stB, err := New(stA.repo.CommonRoot)
	if err != nil {
		t.Fatal(err)
	}

	stale, staleVersion, err := stA.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	winner, winnerVersion, err := stB.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	winner.Title = "winner"
	if _, err := stB.SavePR(winner, winner.Status, winnerVersion); err != nil {
		t.Fatalf("winner SavePR() error = %v", err)
	}

	stale.Title = "loser"
	if _, err := stA.SavePR(stale, stale.Status, staleVersion); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("stale SavePR() error = %v, want ErrMetadataConflict", err)
	}
	loaded, _, err := stA.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "winner" {
		t.Fatalf("stored title = %q, want winner", loaded.Title)
	}
}

func newStoreTestPR(t *testing.T) (*Store, model.PR) {
	t.Helper()
	repoPath := t.TempDir()
	storeTestGit(t, repoPath, "init", "-b", "main")
	storeTestGit(t, repoPath, "config", "user.name", "gitpr tests")
	storeTestGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	storeTestGit(t, repoPath, "commit", "--allow-empty", "-m", "base")
	head := storeTestGit(t, repoPath, "rev-parse", "HEAD")
	st, err := New(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	pr := model.PR{ID: "01TESTMETADATACONFLICT", Title: "initial", SourceHeadSHA: head, BaseHeadSHA: head, Status: model.StatusOpen}
	if _, err := st.SavePR(pr, "", ""); err != nil {
		t.Fatalf("create SavePR() error = %v", err)
	}
	return st, pr
}

func storeTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String())
}
