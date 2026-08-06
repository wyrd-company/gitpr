package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestAddCommentAtSameAnchorAppends(t *testing.T) {
	svc, pr := newTestPR(t)

	first := model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   "first disposition",
	}
	second := model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   "second disposition",
	}

	if _, err := svc.AddComment(pr.ID, first); err != nil {
		t.Fatalf("AddComment(first) error = %v", err)
	}
	updated, err := svc.AddComment(pr.ID, second)
	if err != nil {
		t.Fatalf("AddComment(second) error = %v", err)
	}

	atAnchor := commentsAtAnchor(updated.Comments, first.FilePath, first.LineStart, first.LineEnd)
	if len(atAnchor) != 2 {
		t.Fatalf("comments at anchor = %d, want 2; texts = %v", len(atAnchor), commentTexts(atAnchor))
	}
	if atAnchor[0].Comment != first.Comment {
		t.Fatalf("first comment = %q, want %q", atAnchor[0].Comment, first.Comment)
	}
	if atAnchor[1].Comment != second.Comment {
		t.Fatalf("second comment = %q, want %q", atAnchor[1].Comment, second.Comment)
	}
}

func TestUpdateCommentReplacesByIndex(t *testing.T) {
	svc, pr := newTestPR(t)

	original := model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   "original text",
	}
	updatedPR, err := svc.AddComment(pr.ID, original)
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	createdAt := updatedPR.Comments[0].CreatedAt

	replacement := model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   "edited text",
	}
	updatedPR, err = svc.UpdateComment(pr.ID, 0, replacement)
	if err != nil {
		t.Fatalf("UpdateComment() error = %v", err)
	}

	if len(updatedPR.Comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(updatedPR.Comments))
	}
	if updatedPR.Comments[0].Comment != replacement.Comment {
		t.Fatalf("comment text = %q, want %q", updatedPR.Comments[0].Comment, replacement.Comment)
	}
	if !updatedPR.Comments[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want preserved %v", updatedPR.Comments[0].CreatedAt, createdAt)
	}
}

func TestUpdateCommentRejectsInvalidIndex(t *testing.T) {
	svc, pr := newTestPR(t)

	_, err := svc.UpdateComment(pr.ID, 0, model.Comment{
		FilePath:  "app.txt",
		LineStart: 1,
		LineEnd:   1,
		Comment:   "orphan update",
	})
	if err == nil {
		t.Fatal("UpdateComment() error = nil, want invalid index error")
	}
	if !strings.Contains(err.Error(), "comment index") {
		t.Fatalf("UpdateComment() error = %v, want comment index error", err)
	}
}

func newTestPR(t *testing.T) (*Service, model.PR) {
	t.Helper()

	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "app.txt", "base\n")
	testGit(t, repoPath, "add", "app.txt")
	testGit(t, repoPath, "commit", "-m", "base")
	testGit(t, repoPath, "checkout", "-b", "feature")
	writeTestFile(t, repoPath, "app.txt", "base\nfeature\n")
	testGit(t, repoPath, "add", "app.txt")
	testGit(t, repoPath, "commit", "-m", "feature")

	svc, err := NewService(repoPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	pr, _, err := svc.CreatePR(context.Background(), CreatePRRequest{
		Title:    "test PR",
		Worktree: repoPath,
	})
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	return svc, pr
}

func commentsAtAnchor(comments []model.Comment, filePath string, lineStart, lineEnd int) []model.Comment {
	var matched []model.Comment
	for _, comment := range comments {
		if comment.FilePath == filePath && comment.LineStart == lineStart && comment.LineEnd == lineEnd {
			matched = append(matched, comment)
		}
	}
	return matched
}

func commentTexts(comments []model.Comment) []string {
	texts := make([]string, len(comments))
	for i, comment := range comments {
		texts[i] = comment.Comment
	}
	return texts
}

func testGit(t *testing.T, dir string, args ...string) string {
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

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
