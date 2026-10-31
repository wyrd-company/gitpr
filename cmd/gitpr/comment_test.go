// ---
// relationships:
//
//	validates: branch-based-pull-requests
//
// ---
package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/store"
)

func TestCommentTextInputsPreserveBytes(t *testing.T) {
	const body = " \t`literal` $(command) \"double\" 'single' café\r\n\n  indented\n\n\t "
	for _, input := range []string{"file", "stdin", "text"} {
		for _, kind := range []string{"pr-level", "anchored", "reply"} {
			t.Run(input+"/"+kind, func(t *testing.T) {
				dir := newCLITestRepo(t)
				// Spaces in a nonblank path belong to the path.
				path := filepath.Join(t.TempDir(), " body.txt ")
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				withinDir(t, dir, func() {
					id := strings.Fields(executeCLI(t, "create", "--title", "Input test"))[2]
					args := []string{"comment", id}
					index := 0
					switch kind {
					case "pr-level":
						args = append(args, "--pr-level")
					case "anchored":
						args = append(args, "--file", "sample.txt", "--line-start", "2")
					case "reply":
						out := executeCLI(t, "comment", id, "--pr-level", "--text", "Initial")
						args = append(args, "--thread", strings.Fields(out)[4])
						index = 1
					}
					cmd := newRootCmd()
					var output bytes.Buffer
					cmd.SetOut(&output)
					cmd.SetErr(&output)
					switch input {
					case "file":
						args = append(args, "--text-file", path)
					case "stdin":
						args = append(args, "--text-file", "-")
						cmd.SetIn(strings.NewReader(body))
					case "text":
						args = append(args, "--text", body)
					}
					cmd.SetArgs(args)
					if err := cmd.Execute(); err != nil {
						t.Fatalf("comment: %v\n%s", err, output.String())
					}
					st, err := store.New(dir)
					if err != nil {
						t.Fatal(err)
					}
					pr, _, err := st.LoadPR(id)
					if err != nil {
						t.Fatal(err)
					}
					if len(pr.Threads) != 1 || len(pr.Threads[0].Comments) != index+1 {
						t.Fatalf("unexpected threads: %#v", pr.Threads)
					}
					if got := pr.Threads[0].Comments[index].Body; got != body {
						t.Fatalf("stored body = %q, want %q", got, body)
					}
				})
			})
		}
	}
}

type commentReadError struct{}

func (commentReadError) Read([]byte) (int, error) { return 0, errors.New("injected read failure") }

func TestCommentTextInputErrorsDoNotWrite(t *testing.T) {
	dir := newCLITestRepo(t)
	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	cases := []struct {
		name  string
		flags []string
		in    io.Reader
		want  string
	}{
		{"exclusive", []string{"--text", "inline", "--text-file", path}, nil, "[text text-file]"},
		{"exclusive-empty-text", []string{"--text=", "--text-file", path}, nil, "[text text-file]"},
		{"blank-path", []string{"--text-file", ""}, nil, "--text-file requires a non-blank path"},
		{"whitespace-path", []string{"--text-file", " \t "}, nil, "--text-file requires a non-blank path"},
		{"missing-file", []string{"--text-file", missing}, nil, missing},
		{"directory", []string{"--text-file", filepath.Dir(path)}, nil, "reading --text-file"},
		{"stdin-error", []string{"--text-file", "-"}, commentReadError{}, "injected read failure"},
		{"empty-file", []string{"--text-file", empty}, nil, "non-blank comment text is required"},
		{"blank-stdin", []string{"--text-file", "-"}, strings.NewReader(" \n\t"), "non-blank comment text is required"},
	}
	withinDir(t, dir, func() {
		id := strings.Fields(executeCLI(t, "create", "--title", "Rejected input"))[2]
		ref := "refs/gitpr/pr/" + id + "/meta"
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				before := cliGit(t, dir, "rev-parse", ref)
				cmd := newRootCmd()
				var output bytes.Buffer
				cmd.SetOut(&output)
				cmd.SetErr(&output)
				if tc.in != nil {
					cmd.SetIn(tc.in)
				}
				cmd.SetArgs(append([]string{"comment", id, "--pr-level"}, tc.flags...))
				err := cmd.Execute()
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
				if after := cliGit(t, dir, "rev-parse", ref); after != before {
					t.Fatal("rejected input changed metadata")
				}
				if refs := cliGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+id+"/anchors"); refs != "" {
					t.Fatalf("rejected input wrote anchors: %s", refs)
				}
			})
		}
	})
}

func TestCommentRejectsBlankTextFileBeforeServiceCreation(t *testing.T) {
	withinDir(t, t.TempDir(), func() {
		cmd := newRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"comment", "unused", "--pr-level", "--text-file="})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "non-blank path") {
			t.Fatalf("error = %v, want blank path before repository access", err)
		}
	})
}
