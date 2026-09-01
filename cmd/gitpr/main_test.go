package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
		if _, _, err := st.LoadPR(id); err != nil {
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
		pr, _, _ := st.LoadPR(id)
		if len(pr.Events) != 1 || pr.Events[0].Verdict != model.VerdictAccepted || pr.Events[0].SourceHeadSHA != source || pr.Events[0].BaseHeadSHA != base {
			t.Fatalf("approved events = %#v", pr.Events)
		}
	})
}

func TestBranchCommentThreadCommandsAndAnchorRefsUseStdout(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Thread CLI"))[2]
		commentOut := executeCLI(t, "comment", id, "--file", "sample.txt", "--line-start", "2", "--text", "review body")
		fields := strings.Fields(commentOut)
		if len(fields) < 6 || fields[0] != "Saved" || fields[4] == "" {
			t.Fatalf("comment stdout=%q", commentOut)
		}
		threadID := fields[4]
		for _, leaf := range []string{"head", "base"} {
			if got := cliGit(t, dir, "rev-parse", "--verify", "refs/gitpr/pr/"+id+"/anchors/"+threadID+"/"+leaf); len(got) != 40 {
				t.Fatalf("anchor %s=%q", leaf, got)
			}
		}
		if out := executeCLI(t, "resolve", id, threadID); !strings.Contains(out, "Resolve thread") {
			t.Fatalf("resolve stdout=%q", out)
		}
		executeCLI(t, "comment", id, "--thread", threadID, "--text", "resolved reply")
		if out := executeCLI(t, "reopen", id, threadID); !strings.Contains(out, "Reopen thread") {
			t.Fatalf("reopen stdout=%q", out)
		}
		comments := executeCLI(t, "comments", id)
		for _, want := range []string{"kind: anchored", "status: open", "review body", "resolved reply", "source_head_sha:", "base_head_sha:"} {
			if !strings.Contains(comments, want) {
				t.Errorf("comments missing %q:\n%s", want, comments)
			}
		}
		review := executeCLI(t, "review", id)
		find := func(field string) string {
			match := regexp.MustCompile(`(?m)^    ` + field + `: ([0-9a-f]{40})$`).FindStringSubmatch(review)
			if len(match) != 2 {
				t.Fatalf("missing %s", field)
			}
			return match[1]
		}
		executeCLI(t, "approve", id, "--basis", find("source_head_sha")+":"+find("base_head_sha"))
		if refs := cliGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+id+"/anchors/"+threadID); refs != "" {
			t.Fatalf("anchor refs remain: %s", refs)
		}
		show := executeCLI(t, "show", id)
		for _, want := range []string{"thread_summary:", "open: 1", "outdated: 0"} {
			if !strings.Contains(show, want) {
				t.Errorf("show missing %q", want)
			}
		}
	})
}

func TestCommandHelpUsesBranchBasedSemanticsWithoutApproveMergeAlias(t *testing.T) {
	root := newRootCmd()
	checks := map[string][]string{
		"create":  {"branch-based PR", "worktree branch"},
		"comment": {"comment", "PR thread"},
		"merge":   {"eligible PR", "base branch"},
		"resolve": {"Resolve", "PR comment thread"},
		"reopen":  {"Reopen", "PR comment thread"},
	}
	for name, wants := range checks {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(cmd.Short, want) {
				t.Errorf("%s Short=%q, missing %q", name, cmd.Short, want)
			}
		}
		if name == "merge" && (strings.Contains(strings.ToLower(cmd.Short), "approv") || len(cmd.Aliases) != 0) {
			t.Errorf("merge help implies approval: Short=%q aliases=%v", cmd.Short, cmd.Aliases)
		}
	}
}

func TestBranchBasedMergeCommandAdvancesBaseAndPrintsStdout(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "CLI merge"))[2]
		review := executeCLI(t, "review", id)
		find := func(field string) string {
			re := regexp.MustCompile(`(?m)^    ` + field + `: ([0-9a-f]{40})$`)
			match := re.FindStringSubmatch(review)
			if len(match) != 2 {
				t.Fatalf("missing %s", field)
			}
			return match[1]
		}
		source, base := find("source_head_sha"), find("base_head_sha")
		executeCLI(t, "approve", id, "--basis", source+":"+base)
		out := executeCLI(t, "merge", id)
		if !strings.Contains(out, "Merged PR") || !strings.Contains(out, "Source worktree kept.") {
			t.Fatalf("merge stdout = %q", out)
		}
		if got := cliGit(t, dir, "rev-parse", "refs/heads/main"); got != source {
			t.Fatalf("base = %s, want %s", got, source)
		}
		st, _ := store.New(dir)
		pr, _, _ := st.LoadPR(id)
		if pr.State != model.PRStateMerged {
			t.Fatalf("state = %s", pr.State)
		}
	})
}

func TestCloseReasonListingAndDeleteCommandsUseStdout(t *testing.T) {
	policyCmd := newRootCmd()
	if !policyCmd.SilenceUsage || !policyCmd.SilenceErrors {
		t.Fatalf("root error policy: SilenceUsage=%t SilenceErrors=%t", policyCmd.SilenceUsage, policyCmd.SilenceErrors)
	}

	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Lifecycle"))[2]
		out := executeCLI(t, "close", id, "--reason", "abandoned", "--note", "not proceeding")
		if !strings.Contains(out, "Closed PR") || !strings.Contains(out, "abandoned") {
			t.Fatalf("close stdout=%q", out)
		}
		if got := executeCLI(t, "list"); !strings.Contains(got, "No PRs found.") {
			t.Fatalf("default list=%q", got)
		}
		listed := executeCLI(t, "list", "--state", "closed", "--reason", "abandoned")
		if !strings.Contains(listed, "Lifecycle") || !strings.Contains(listed, "closed") {
			t.Fatalf("reason list=%q", listed)
		}
		show := executeCLI(t, "show", id)
		for _, want := range []string{"reason: abandoned", "note: not proceeding"} {
			if !strings.Contains(show, want) {
				t.Errorf("show missing %q", want)
			}
		}
		previewCmd := newRootCmd()
		var previewOut, previewErr bytes.Buffer
		previewCmd.SetOut(&previewOut)
		previewCmd.SetErr(&previewErr)
		previewCmd.SetArgs([]string{"delete", id})
		deleteErr := previewCmd.Execute()
		if deleteErr == nil || !strings.Contains(deleteErr.Error(), "--force") {
			t.Fatalf("unguarded delete error=%v", deleteErr)
		}
		if previewErr.Len() != 0 {
			t.Fatalf("Cobra wrote refusal stderr=%q", previewErr.String())
		}
		for _, want := range []string{"Would delete PR", "state: closed", "events: 0", "threads: 0", "pinned review commits may become collectable"} {
			if !strings.Contains(previewOut.String(), want) {
				t.Errorf("delete preview missing %q: %s", want, previewOut.String())
			}
		}
		fmt.Fprintln(&previewErr, deleteErr)
		if got := previewErr.String(); strings.Count(got, "refusing to delete without --force") != 1 || strings.Contains(got, "Usage:") {
			t.Fatalf("main-owned error output=%q", got)
		}
		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.LoadPR(id); err != nil {
			t.Fatalf("preview deleted record: %v", err)
		}
		if out := executeCLI(t, "delete", id, "--force"); !strings.Contains(out, "Deleted PR") {
			t.Fatalf("delete stdout=%q", out)
		}
		if refs := cliGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr"); strings.Contains(refs, id) {
			t.Fatalf("refs remain: %s", refs)
		}
	})
}

func TestReasonMisuseNamesTheFilterFlagActuallySupplied(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		for _, tc := range []struct {
			args []string
			want string
		}{{[]string{"list", "--state", "open", "--reason", "abandoned"}, "--state closed"}, {[]string{"list", "--status", "open", "--reason", "abandoned"}, "--status closed"}} {
			cmd := newRootCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("gitpr %v error=%v, want %q", tc.args, err, tc.want)
			}
		}
	})
}

func TestListSkipsLegacyRecordsAndIDVerbsNameTheDocumentedPath(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		legacyID := writeCLILegacyRecord(t, dir, "01LEGACYCLIRECORD000000000")

		empty := executeCLI(t, "list", "--state", "open")
		if !strings.Contains(empty, "No PRs found.") || !strings.Contains(empty, "Skipped 1") || !strings.Contains(empty, "Legacy records") {
			t.Fatalf("empty list output = %q", empty)
		}

		id := strings.Fields(executeCLI(t, "create", "--title", "Branch record"))[2]
		listed := executeCLI(t, "list", "--state", "open")
		if !strings.Contains(listed, "Branch record") || strings.Contains(listed, legacyID[:12]) {
			t.Fatalf("list rendered a legacy record:\n%s", listed)
		}
		if !strings.Contains(listed, "Skipped 1") || !strings.Contains(listed, "docs/usage.md") {
			t.Fatalf("list did not report the skipped legacy record:\n%s", listed)
		}

		for _, args := range [][]string{{"show", legacyID}, {"comments", legacyID}, {"review", legacyID}, {"merge", legacyID}, {"delete", legacyID, "--force"}} {
			cmd := newRootCmd()
			cmd.SetArgs(args)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "Legacy records") || !strings.Contains(err.Error(), "gitpr create") {
				t.Errorf("gitpr %v error = %v, want the documented legacy guidance", args, err)
			}
		}

		show := executeCLI(t, "show", id)
		for _, want := range []string{"schema: 2", "state: open", "source_branch: feature", "thread_summary:"} {
			if !strings.Contains(show, want) {
				t.Errorf("show missing %q:\n%s", want, show)
			}
		}
	})
}

func TestListFiltersRefuseTheLegacyVocabulary(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		for _, state := range []string{"approved", "rejected"} {
			cmd := newRootCmd()
			cmd.SetArgs([]string{"list", "--state", state})
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported status filter") {
				t.Errorf("gitpr list --state %s error = %v, want an unsupported-filter refusal", state, err)
			}
		}
	})
}

func TestRemovedLegacyVerbsAndFlagsAreAbsent(t *testing.T) {
	root := newRootCmd()
	if cmd, _, err := root.Find([]string{"refresh"}); err == nil && cmd.Name() == "refresh" {
		t.Error("refresh command is still registered")
	}
	comment, _, err := root.Find([]string{"comment"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"update", "commit"} {
		if comment.Flags().Lookup(flag) != nil {
			t.Errorf("comment still exposes --%s", flag)
		}
	}
}

func writeCLILegacyRecord(t *testing.T, dir, id string) string {
	t.Helper()
	head := cliGit(t, dir, "rev-parse", "HEAD")
	document := "id: " + id + "\ntitle: legacy snapshot\nsource_branch: feature\nbase_branch: main\n" +
		"source_head_sha: " + head + "\nbase_head_sha: " + head + "\nstatus: open\n"
	blob := cliGitStdin(t, dir, document, "hash-object", "-w", "--stdin")
	tree := cliGitStdin(t, dir, "100644 blob "+blob+"\tpr.yaml\n", "mktree")
	commit := cliGitStdin(t, dir, "", "commit-tree", tree, "-m", "legacy record")
	cliGit(t, dir, "update-ref", "refs/gitpr/pr/"+id+"/meta", commit)
	cliGit(t, dir, "update-ref", "refs/gitpr/index/open/"+id, commit)
	return id
}

func cliGitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String())
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

// TestDocumentedLegacyCommandsEnumerateAndRemoveARecord executes the shell
// blocks the refusal error points people at, so the prose and the behavior
// cannot drift apart.
func TestDocumentedLegacyCommandsEnumerateAndRemoveARecord(t *testing.T) {
	for _, doc := range []string{"../../docs/usage.md", "../../README.md"} {
		t.Run(doc, func(t *testing.T) {
			enumerate := documentedShellBlock(t, doc, "for-each-ref", "schema:")
			remove := documentedShellBlock(t, doc, "update-ref -d")

			dir := newCLITestRepo(t)
			legacyID := writeCLILegacyRecord(t, dir, "01LEGACYDOCCOMMANDS0000000")
			var keptID string
			withinDir(t, dir, func() {
				keptID = strings.Fields(executeCLI(t, "create", "--title", "Kept record"))[2]
			})

			listed := runShell(t, dir, enumerate)
			if listed != "refs/gitpr/pr/"+legacyID+"/meta" {
				t.Fatalf("documented enumeration = %q, want the legacy meta ref only", listed)
			}

			// A nested pin proves the documented pattern reaches deeper than one
			// path component; a single-star glob silently matches only meta.
			nested := "refs/gitpr/pr/" + legacyID + "/events/01NESTEDEVENTPIN0000000000/head"
			cliGit(t, dir, "update-ref", nested, cliGit(t, dir, "rev-parse", "HEAD"))

			runShell(t, dir, strings.ReplaceAll(remove, "<id>", legacyID))
			if refs := cliGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+legacyID, "refs/gitpr/index/*/"+legacyID); refs != "" {
				t.Fatalf("documented removal left refs: %q", refs)
			}
			if refs := cliGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+keptID+"/meta"); refs == "" {
				t.Fatal("documented removal also removed the branch-based record")
			}
		})
	}
}

func documentedShellBlock(t *testing.T, path string, required ...string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.Split(string(source), "## Legacy records")
	if len(sections) != 2 {
		t.Fatalf("%s has no single \"Legacy records\" section", path)
	}
	for _, block := range strings.Split(sections[1], "```") {
		if !strings.HasPrefix(block, "bash\n") {
			continue
		}
		body := strings.TrimPrefix(block, "bash\n")
		matched := true
		for _, want := range required {
			if !strings.Contains(body, want) {
				matched = false
				break
			}
		}
		if matched {
			return body
		}
	}
	t.Fatalf("%s has no documented shell block containing %v", path, required)
	return ""
}

func runShell(t *testing.T, dir, script string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("documented command failed: %s\n%s: %v", script, stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String())
}

// idCommandInvocations names the arguments each registered PR-id command needs
// to reach its record load. TestEveryRegisteredIDCommandRefusesALegacyRecord
// asserts this map covers the command tree exactly, so a new id verb cannot be
// added without deciding how it treats a legacy record.
func idCommandInvocations(t *testing.T, legacyID string) map[string][]string {
	t.Helper()
	const sourceHead = "0123456789abcdef0123456789abcdef01234567"
	const baseHead = "89abcdef0123456789abcdef0123456789abcdef"
	return map[string][]string{
		"show":         {"show", legacyID},
		"comments":     {"comments", legacyID},
		"review":       {"review", legacyID},
		"approve":      {"approve", legacyID, "--basis", sourceHead + ":" + baseHead},
		"reject":       {"reject", legacyID, "--basis", sourceHead + ":" + baseHead},
		"merge":        {"merge", legacyID},
		"comment":      {"comment", legacyID, "--pr-level", "--text", "refused"},
		"resolve":      {"resolve", legacyID, "01THREADIDNOTREACHED000000"},
		"reopen":       {"reopen", legacyID, "01THREADIDNOTREACHED000000"},
		"close":        {"close", legacyID, "--reason", "abandoned"},
		"delete":       {"delete", legacyID, "--force"},
		"debug export": {"debug", "export", legacyID, "--to", t.TempDir()},
	}
}

func TestEveryRegisteredIDCommandRefusesALegacyRecord(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		legacyID := writeCLILegacyRecord(t, dir, "01LEGACYREGISTRYSWEEP00000")
		before := cliGit(t, dir, "show", "refs/gitpr/pr/"+legacyID+"/meta:pr.yaml")
		invocations := idCommandInvocations(t, legacyID)

		registered := map[string]struct{}{}
		var walk func(cmd *cobra.Command, prefix string)
		walk = func(cmd *cobra.Command, prefix string) {
			for _, child := range cmd.Commands() {
				name := strings.TrimSpace(prefix + " " + child.Name())
				if strings.Contains(child.Use, "pr-id") {
					registered[name] = struct{}{}
				}
				walk(child, name)
			}
		}
		walk(newRootCmd(), "")

		for name := range registered {
			if _, covered := invocations[name]; !covered {
				t.Errorf("command %q takes a PR id but is not exercised against a legacy record", name)
			}
		}
		for name := range invocations {
			if _, exists := registered[name]; !exists {
				t.Errorf("invocation %q names no registered PR-id command", name)
			}
		}

		for name, args := range invocations {
			cmd := newRootCmd()
			cmd.SetArgs(args)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			err := cmd.Execute()
			if err == nil {
				t.Errorf("gitpr %v succeeded on a legacy record", args)
				continue
			}
			for _, want := range []string{"gitpr create", "update-ref -d", "github.com/wyrd-company/gitpr"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s refusal %q is missing %q", name, err, want)
				}
			}
		}

		if after := cliGit(t, dir, "show", "refs/gitpr/pr/"+legacyID+"/meta:pr.yaml"); after != before {
			t.Fatalf("a refused command changed the legacy record\n before: %s\n after: %s", before, after)
		}
	})
}
