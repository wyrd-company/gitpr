package store

import (
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os/exec"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestProductionStoreAPIExcludesBeforeSaveHook(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	files := append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv != nil && function.Name.Name == "SetBeforeSaveHook" {
				t.Fatalf("production file %s exports SetBeforeSaveHook", name)
			}
		}
	}
}

func TestStableGitEnvForcesCMessageLocale(t *testing.T) {
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	env := stableGitEnv()
	var locales []string
	for _, value := range env {
		if strings.HasPrefix(value, "LC_ALL=") {
			locales = append(locales, value)
		}
	}
	if len(locales) != 1 || locales[0] != "LC_ALL=C" {
		t.Fatalf("LC_ALL entries = %v, want [LC_ALL=C]", locales)
	}
}

func TestRefConflictClassificationUsesStableSpecificMessages(t *testing.T) {
	if !isRefConflict(errors.New("cannot lock ref 'refs/example'")) {
		t.Fatal("cannot-lock-ref error was not classified as a conflict")
	}
	if !isRefConflict(errors.New("reference already exists")) {
		t.Fatal("existing-reference error was not classified as a conflict")
	}
	if isRefConflict(errors.New("metadata is at an unexpected value")) {
		t.Fatal("generic 'is at' text was classified as a ref conflict")
	}
}

func TestResolveRefForLoadDistinguishesMissingRefFromGitFailure(t *testing.T) {
	st, _ := newStoreTestPR(t)
	if _, exists, err := st.resolveRefForLoad("refs/gitpr/pr/missing/meta"); err != nil || exists {
		t.Fatalf("missing ref: exists = %v, error = %v; want false, nil", exists, err)
	}

	nonRepo := t.TempDir()
	st.repo.CommonRoot = nonRepo
	_, _, err := st.resolveRefForLoad("refs/gitpr/pr/missing/meta")
	if err == nil {
		t.Fatal("non-repository resolve error = nil, want verbatim git failure")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("resolve error = %q, want underlying git failure", err)
	}
}

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
