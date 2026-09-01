package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func TestCreateAndReviewCommandsUseSchema2BasisYAML(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		createOut := executeCLI(t, "create", "--title", "CLI branch record", "--description", "review live heads")
		fields := strings.Fields(createOut)
		if len(fields) < 3 || fields[0] != "Created" || fields[1] != "PR" {
			t.Fatalf("create output = %q", createOut)
		}
		id := fields[2]
		reviewOut := executeCLI(t, "review", id)
		for _, want := range []string{"basis:", "source_head_sha:", "base_head_sha:", "base_contained: true", "sample.txt", "+feature"} {
			if !strings.Contains(reviewOut, want) {
				t.Errorf("review output missing %q:\n%s", want, reviewOut)
			}
		}
		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.LoadPR2(id); err != nil {
			t.Fatalf("created record is not schema 2: %v", err)
		}
	})
}

func TestListAndShowRenderMixedShapesWhileLegacyOutputStaysStable(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		head := cliGit(t, dir, "rev-parse", "HEAD")
		legacy := model.PR{ID: "01LEGACYCLIGOLDEN000000000", Title: "Legacy record", SourceBranch: "feature", SourceHeadSHA: head, BaseHeadSHA: head, Status: model.StatusOpen}
		if _, err := st.SavePR(legacy, "", ""); err != nil {
			t.Fatal(err)
		}
		legacyWant := "ID             STATUS     BRANCH               TITLE\n01LEGACYCLIG   open       feature              Legacy record\n"
		if got := executeCLI(t, "list", "--status", "open"); got != legacyWant {
			t.Fatalf("legacy list output changed\n got: %q\nwant: %q", got, legacyWant)
		}
		loadedLegacy, _, err := st.LoadLegacyPR(legacy.ID)
		if err != nil {
			t.Fatal(err)
		}
		legacyYAML, err := yaml.Marshal(loadedLegacy)
		if err != nil {
			t.Fatal(err)
		}
		if got := executeCLI(t, "show", legacy.ID); got != string(legacyYAML) {
			t.Fatalf("legacy show output changed\n got: %q\nwant: %q", got, legacyYAML)
		}

		createOut := executeCLI(t, "create", "--title", "Branch record")
		id := strings.Fields(createOut)[2]
		mixed := executeCLI(t, "list", "--status", "open")
		for _, want := range []string{"Legacy record", "Branch record", "open"} {
			if !strings.Contains(mixed, want) {
				t.Errorf("mixed list missing %q:\n%s", want, mixed)
			}
		}
		show := executeCLI(t, "show", id)
		for _, want := range []string{"schema: 2", "state: open", "source_branch: feature", "source_worktree_path:"} {
			if !strings.Contains(show, want) {
				t.Errorf("schema-2 show missing %q:\n%s", want, show)
			}
		}
	})
}

func executeCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newRootCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gitpr %s: %v\n%s", strings.Join(args, " "), err, output.String())
	}
	return output.String()
}

func newCLITestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cliGit(t, dir, "init", "-b", "main")
	cliGit(t, dir, "config", "user.name", "gitpr tests")
	cliGit(t, dir, "config", "user.email", "gitpr@example.test")
	if err := os.WriteFile(dir+"/sample.txt", []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, dir, "add", "sample.txt")
	cliGit(t, dir, "commit", "-m", "base")
	cliGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(dir+"/sample.txt", []byte("base\nfeature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, dir, "add", "sample.txt")
	cliGit(t, dir, "commit", "-m", "feature")
	return dir
}

func withinDir(t *testing.T, dir string, run func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	run()
}

func cliGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}
