package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/store"
)

// shellFragileDescription mirrors the content observed corrupting shell-quoted
// --description values in tasks 298, 549, and 658: backticks, a $() command
// substitution marker, embedded quotes, and multiple lines.
const shellFragileDescription = "line one has a backtick `date` inside\n" +
	"line two with $(command) and \"quotes\" and 'single quotes'\n" +
	"line three trailing"

func TestCreateDescriptionFileReadsContentByteForByte(t *testing.T) {
	dir := newCLITestRepo(t)
	descPath := filepath.Join(t.TempDir(), "description.txt")
	if err := os.WriteFile(descPath, []byte(shellFragileDescription), 0o644); err != nil {
		t.Fatal(err)
	}

	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "File description", "--description-file", descPath))[2]

		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		pr, _, err := st.LoadPR(id)
		if err != nil {
			t.Fatal(err)
		}
		if pr.Description != shellFragileDescription {
			t.Fatalf("stored description = %q, want %q", pr.Description, shellFragileDescription)
		}
	})
}

func TestCreateDescriptionFilePreservesLeadingTrailingWhitespaceAndBlankLines(t *testing.T) {
	dir := newCLITestRepo(t)
	const whitespaceSensitive = "first line\n  indented line\n\ntrailing blank line follows\n\n"
	descPath := filepath.Join(t.TempDir(), "whitespace.txt")
	if err := os.WriteFile(descPath, []byte(whitespaceSensitive), 0o644); err != nil {
		t.Fatal(err)
	}

	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Whitespace", "--description-file", descPath))[2]

		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		pr, _, err := st.LoadPR(id)
		if err != nil {
			t.Fatal(err)
		}
		if pr.Description != whitespaceSensitive {
			t.Fatalf("stored description = %q, want %q", pr.Description, whitespaceSensitive)
		}
	})
}

func TestCreateDescriptionFileDashReadsStandardInputByteForByte(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetIn(strings.NewReader(shellFragileDescription))
		cmd.SetArgs([]string{"create", "--title", "Stdin description", "--description-file", "-"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("create via stdin: %v, stderr=%s", err, stderr.String())
		}
		fields := strings.Fields(stdout.String())
		if len(fields) < 3 {
			t.Fatalf("create stdout = %q", stdout.String())
		}
		id := fields[2]

		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		pr, _, err := st.LoadPR(id)
		if err != nil {
			t.Fatal(err)
		}
		if pr.Description != shellFragileDescription {
			t.Fatalf("stored description = %q, want %q", pr.Description, shellFragileDescription)
		}
	})
}

func TestCreateRejectsDescriptionAndDescriptionFileTogether(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"create", "--title", "Both", "--description", "inline", "--description-file", "/nonexistent"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "description") || !strings.Contains(err.Error(), "description-file") {
			t.Fatalf("create with both description flags error = %v", err)
		}
	})
}

func TestCreateDescriptionFileErrorsClearlyOnUnreadablePath(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"create", "--title", "Missing file", "--description-file", "/does/not/exist.txt"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "/does/not/exist.txt") {
			t.Fatalf("create with unreadable --description-file error = %v", err)
		}

		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		open, _, err := st.ListPRs("open")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 0 {
			t.Fatalf("create wrote a record despite the unreadable description file: %#v", open)
		}
	})
}

func TestEditCommandReplacesDescriptionOnOpenPRAndDocumentsFileFlow(t *testing.T) {
	dir := newCLITestRepo(t)
	descPath := filepath.Join(t.TempDir(), "correction.txt")
	if err := os.WriteFile(descPath, []byte(shellFragileDescription), 0o644); err != nil {
		t.Fatal(err)
	}

	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Correctable"))[2]

		out := executeCLI(t, "edit", id, "--description-file", descPath)
		if !strings.Contains(out, "Edited PR") {
			t.Fatalf("edit stdout = %q", out)
		}

		st, err := store.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		pr, _, err := st.LoadPR(id)
		if err != nil {
			t.Fatal(err)
		}
		if pr.Description != shellFragileDescription {
			t.Fatalf("edited description = %q, want %q", pr.Description, shellFragileDescription)
		}

		open, _, err := st.ListPRs("open")
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 1 {
			t.Fatalf("open PRs after edit = %d, want 1 (no second snapshot)", len(open))
		}
	})
}

func TestEditCommandRequiresAtLeastOneFlag(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "No-op edit"))[2]
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"edit", id})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "requires") {
			t.Fatalf("edit with no flags error = %v", err)
		}
	})
}

func TestEditCommandRejectsClosedPRWithoutWriting(t *testing.T) {
	dir := newCLITestRepo(t)
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Will close"))[2]
		executeCLI(t, "close", id, "--reason", "abandoned")

		beforeDoc := cliGit(t, dir, "show", "refs/gitpr/pr/"+id+"/meta:pr.yaml")

		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"edit", id, "--description", "after close"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("edit on closed PR error = %v", err)
		}

		afterDoc := cliGit(t, dir, "show", "refs/gitpr/pr/"+id+"/meta:pr.yaml")
		if afterDoc != beforeDoc {
			t.Fatalf("rejected edit changed the closed record:\n before: %s\n after: %s", beforeDoc, afterDoc)
		}
	})
}
