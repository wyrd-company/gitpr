package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

// TestEveryIDVerbRefusesALegacyRecord holds the contract that gitpr neither
// reads nor mutates a schema-absent record. Every verb that takes a PR id is
// listed here; a new id verb belongs in this table.
func TestEveryIDVerbRefusesALegacyRecord(t *testing.T) {
	repoPath, service := newBranchService(t)
	legacyID := writeLegacyRecord(t, repoPath, "01LEGACYVERBREFUSAL0000000")
	before := legacyRecordBytes(t, repoPath, legacyID)

	verbs := []struct {
		name string
		call func() error
	}{
		{"show", func() error { _, _, err := service.LoadRecord(legacyID); return err }},
		{"review", func() error { _, err := service.ReviewPR(context.Background(), legacyID); return err }},
		{"approve", func() error {
			_, _, err := service.ApprovePR(context.Background(), legacyID, ExpectedHeads{Source: strings.Repeat("a", 40), Base: strings.Repeat("b", 40)})
			return err
		}},
		{"reject", func() error {
			_, _, err := service.RejectPR(context.Background(), legacyID, &ExpectedHeads{Source: strings.Repeat("a", 40), Base: strings.Repeat("b", 40)})
			return err
		}},
		{"merge", func() error { _, _, err := service.MergePR(context.Background(), legacyID, false); return err }},
		{"close", func() error {
			_, _, err := service.ClosePR(legacyID, ClosePRRequest{Reason: model.ClosureAbandoned})
			return err
		}},
		{"comment", func() error {
			_, _, err := service.CommentPR2(context.Background(), legacyID, ThreadCommentRequest{PRLevel: true, Text: "no"})
			return err
		}},
		{"resolve", func() error {
			_, _, err := service.SetThreadStatus(legacyID, "thread", model.ThreadResolved)
			return err
		}},
		{"reopen", func() error { _, _, err := service.SetThreadStatus(legacyID, "thread", model.ThreadOpen); return err }},
		{"delete-preview", func() error { _, err := service.DeleteRecordSummary(legacyID); return err }},
		{"delete", func() error { return service.DeleteRecord(legacyID) }},
	}
	for _, verb := range verbs {
		err := verb.call()
		if !errors.Is(err, store.ErrLegacyRecord) {
			t.Errorf("%s on a legacy record = %v, want store.ErrLegacyRecord", verb.name, err)
			continue
		}
		if !strings.Contains(err.Error(), "Legacy records") {
			t.Errorf("%s refusal %q does not name the documented section", verb.name, err)
		}
	}

	if after := legacyRecordBytes(t, repoPath, legacyID); after != before {
		t.Fatalf("legacy record changed\n before: %s\n after: %s", before, after)
	}
}

func TestListSkipsLegacyRecordsAndReportsTheSkipCount(t *testing.T) {
	repoPath, service := newBranchService(t)
	writeLegacyRecord(t, repoPath, "01LEGACYLISTSKIP0000000000")
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Listed", Worktree: repoPath})
	if err != nil {
		t.Fatal(err)
	}

	records, skipped, err := service.ListPRs("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != pr.ID {
		t.Fatalf("open records = %#v, want the branch-based record only", records)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	message := SkippedRecordsMessage(skipped)
	for _, want := range []string{"Skipped 1", "Legacy records", "docs/usage.md"} {
		if !strings.Contains(message, want) {
			t.Errorf("skip message %q is missing %q", message, want)
		}
	}
}

// writeLegacyRecord creates a schema-absent record with raw git plumbing,
// which is the only way one can exist now that no write path produces them.
func writeLegacyRecord(t *testing.T, repoPath, id string) string {
	t.Helper()
	head := testGit(t, repoPath, "rev-parse", "HEAD")
	document := "id: " + id + "\ntitle: legacy snapshot\nsource_branch: topic\nbase_branch: main\n" +
		"source_head_sha: " + head + "\nbase_head_sha: " + head + "\nstatus: open\n"
	blob := testGitStdin(t, repoPath, document, "hash-object", "-w", "--stdin")
	tree := testGitStdin(t, repoPath, "100644 blob "+blob+"\tpr.yaml\n", "mktree")
	commit := testGitStdin(t, repoPath, "", "commit-tree", tree, "-m", "legacy record")
	testGit(t, repoPath, "update-ref", "refs/gitpr/pr/"+id+"/meta", commit)
	testGit(t, repoPath, "update-ref", "refs/gitpr/index/open/"+id, commit)
	return id
}

func legacyRecordBytes(t *testing.T, repoPath, id string) string {
	t.Helper()
	return testGit(t, repoPath, "show", "refs/gitpr/pr/"+id+"/meta:pr.yaml")
}

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return testGitStdin(t, dir, "", args...)
}

func testGitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout strings.Builder
	var stderr strings.Builder
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String())
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
