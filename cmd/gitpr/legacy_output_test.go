/*
---
relationships: {validates: ../../docs/technical-designs/branch-based-pull-requests.yml, references: ../../docs/user/usage.md}
---
*/
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise main as a subprocess: handler tests cannot establish the actual
// stdout/stderr routing or exit status seen by someone running gitpr.
func TestBinaryReportsLegacySkipCountAndRecoveryGuidance(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gitpr")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build gitpr: %s: %v", out, err)
	}
	run := func(dir string, args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("branch_record=%t", mixed), func(t *testing.T) {
			dir := newCLITestRepo(t)
			if mixed {
				out, stderr, err := run(dir, "create", "--title", "Supported record")
				if err != nil || stderr != "" {
					t.Fatalf("create: stdout=%q stderr=%q error=%v", out, stderr, err)
				}
			}

			for count := 0; count <= 2; count++ {
				if count > 0 {
					id := fmt.Sprintf("01LEGACYOUTPUT%013d", count)
					writeCLILegacyRecord(t, dir, id)
				}
				before := cliGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
				out, stderr, err := run(dir, "list")
				if err != nil || stderr != "" {
					t.Fatalf("list: stdout=%q stderr=%q error=%v", out, stderr, err)
				}
				if mixed {
					if !strings.Contains(out, "Supported record") || strings.Contains(out, "No PRs found.") {
						t.Fatalf("list lost supported record: %q", out)
					}
				} else if !strings.Contains(out, "No PRs found.") {
					t.Fatalf("legacy-only list: %q", out)
				}
				if strings.Contains(out, "legacy snapshot") || strings.Contains(out, "01LEGACY") {
					t.Fatalf("list rendered a legacy record: %q", out)
				}
				if count == 0 {
					if strings.Contains(out, "Skipped") {
						t.Fatalf("list reported skips without legacy records: %q", out)
					}
				} else {
					want := fmt.Sprintf("Skipped %d unreadable record(s) (legacy or newer schema).", count)
					if strings.Count(out, want) != 1 || !strings.Contains(out, "Legacy records") || !strings.Contains(out, "docs/user/usage.md") {
						t.Fatalf("list missing skip count or guidance: %q", out)
					}

					id := fmt.Sprintf("01LEGACYOUTPUT%013d", count)
					out, stderr, err = run(dir, "show", id)
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || out != "" {
						t.Fatalf("show: stdout=%q stderr=%q error=%v; want exit 1 and no stdout", out, stderr, err)
					}
					for _, want := range []string{
						"legacy snapshot", "git show refs/gitpr/pr/" + id + "/meta:pr.yaml",
						"gitpr create", "git for-each-ref", "git update-ref -d", "irreversible",
						"Legacy records", "docs/user/usage.md",
					} {
						if !strings.Contains(stderr, want) {
							t.Errorf("show refusal missing %q: %s", want, stderr)
						}
					}
				}
				if after := cliGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr"); after != before {
					t.Fatalf("list or refused show changed records:\nbefore: %s\nafter: %s", before, after)
				}
			}
		})
	}
}
