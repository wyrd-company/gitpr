package main

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

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
		for _, want := range []string{"basis:", "source_head_sha:", "base_head_sha:", "base_contained: true", "sample.txt", "+feature", "verdict_hint: gitpr approve"} {
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

func TestReviewBasisCanBePastedIntoApproveCommandOnStdout(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Verdict flow"))[2]
		review := executeCLI(t, "review", id)
		find := func(field string) string {
			re := regexp.MustCompile(`(?m)^    ` + field + `: ([0-9a-f]{40})$`)
			match := re.FindStringSubmatch(review)
			if len(match) != 2 {
				t.Fatalf("review output missing %s:\n%s", field, review)
			}
			return match[1]
		}
		source, base := find("source_head_sha"), find("base_head_sha")
		out := executeCLI(t, "approve", id, "--basis", source+":"+base)
		if !strings.Contains(out, "Approved PR") {
			t.Fatalf("approve stdout = %q", out)
		}
		st, _ := store.New(dir)
		pr, _, _ := st.LoadPR2(id)
		if len(pr.Events) != 1 || pr.Events[0].Verdict != model.VerdictAccepted || pr.Events[0].SourceHeadSHA != source || pr.Events[0].BaseHeadSHA != base {
			t.Fatalf("approved events = %#v", pr.Events)
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
		hash := exec.Command("git", "-C", dir, "hash-object", "-w", "--stdin")
		if _, err := hash.Output(); err != nil {
			t.Fatal(err)
		}
		const objectID = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
		legacy := model.PR{ID: "01LEGACYCLIGOLDEN000000000", Title: "Legacy record", SourceBranch: "feature", BaseBranch: "main", SourceHeadSHA: objectID, BaseHeadSHA: objectID, Status: model.StatusOpen}
		if _, err := st.SavePR(legacy, "", ""); err != nil {
			t.Fatal(err)
		}
		legacyWant := "ID             STATUS     BRANCH               TITLE\n01LEGACYCLIG   open       feature              Legacy record\n"
		if got := executeCLI(t, "list", "--status", "open"); got != legacyWant {
			t.Fatalf("legacy list output changed\n got: %q\nwant: %q", got, legacyWant)
		}
		const legacyYAML = "id: 01LEGACYCLIGOLDEN000000000\ntitle: Legacy record\nsource_branch: feature\nsource_worktree_path: \"\"\nrepository_root: \"\"\nbase_branch: main\nsource_head_sha: e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\nbase_head_sha: e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\ndescription: \"\"\nfile_diffs: []\ncommits: []\nstatus: open\ncreated_at: 0001-01-01T00:00:00Z\nupdated_at: 0001-01-01T00:00:00Z\n"
		if got := executeCLI(t, "show", legacy.ID); got != legacyYAML {
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

func TestMainRoutesCommandDataToStdout(t *testing.T) {
	binary := t.TempDir() + "/gitpr"
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gitpr: %s: %v", output, err)
	}
	dir := newCLITestRepo(t)
	cmd := exec.Command(binary, "list", "--status", "open")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gitpr list: %v, stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No PRs found.") || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestVerdictCommandsRejectPartialOrConflictingBasisFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"approve", "anything", "--source-head", "one"}, want: "both --source-head and --base-head"},
		{args: []string{"reject", "anything", "--base-head", "two"}, want: "both --source-head and --base-head"},
		{args: []string{"approve", "anything", "--basis", "one"}, want: "--basis must be"},
		{args: []string{"approve", "anything", "--basis", "feature:main"}, want: "paste the basis"},
		{args: []string{"reject", "anything", "--source-head", "feature", "--base-head", "main"}, want: "paste the basis"},
		{args: []string{"approve", "anything", "--basis", "one:two", "--source-head", "one", "--base-head", "two"}, want: "either --basis"},
	} {
		root := newRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(tc.args)
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("gitpr %v error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func executeCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gitpr %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("gitpr %s wrote data to stderr: %q", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
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
