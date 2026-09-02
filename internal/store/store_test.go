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
	st, _ := newStoreTestLegacyPR(t)
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

func TestResolveRefForLoadUsesOneGitProcess(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "store.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "resolveRefForLoad" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "runGit" {
				calls++
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("resolveRefForLoad git process count = %d, want 1", calls)
	}
}

// newStoreTestLegacyPR builds a repository holding one schema-absent record.
// The record is written with raw git plumbing because gitpr no longer has a
// legacy write path.
func newStoreTestLegacyPR(t *testing.T) (*Store, string) {
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
	id := "01LEGACYSNAPSHOT0000000000"
	writeRawPRRecord(t, st, id, "open", []byte(legacyRecordYAML(id, head)))
	return st, id
}

func legacyRecordYAML(id, head string) string {
	return "id: " + id + "\ntitle: legacy snapshot\nsource_branch: topic\nbase_branch: main\n" +
		"source_head_sha: " + head + "\nbase_head_sha: " + head + "\nstatus: open\n"
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
