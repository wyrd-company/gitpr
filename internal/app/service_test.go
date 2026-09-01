package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

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

func TestUpdateCommentRejectsAnchorMismatch(t *testing.T) {
	svc, pr := newTestPR(t)

	original := model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   "original text",
	}
	if _, err := svc.AddComment(pr.ID, original); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	_, err := svc.UpdateComment(pr.ID, 0, model.Comment{
		FilePath:  "elsewhere.txt",
		LineStart: 99,
		LineEnd:   99,
		Comment:   "moved comment",
	})
	if err == nil {
		t.Fatal("UpdateComment() error = nil, want anchor mismatch error")
	}
	if !strings.Contains(err.Error(), "anchor mismatch") {
		t.Fatalf("UpdateComment() error = %v, want anchor mismatch error", err)
	}

	loaded, _, err := svc.LoadPR(pr.ID)
	if err != nil {
		t.Fatalf("LoadPR() error = %v", err)
	}
	if len(loaded.Comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(loaded.Comments))
	}
	if loaded.Comments[0].FilePath != original.FilePath || loaded.Comments[0].LineStart != original.LineStart {
		t.Fatalf("comment anchor changed to %s:%d-%d, want preserved %s:%d-%d",
			loaded.Comments[0].FilePath, loaded.Comments[0].LineStart, loaded.Comments[0].LineEnd,
			original.FilePath, original.LineStart, original.LineEnd)
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

func TestLegacyCommentMutationsPreserveOrderAndCardinality(t *testing.T) {
	service, pr := newTestPR(t)
	for _, text := range []string{"first", "second", "third"} {
		if _, err := service.AddComment(pr.ID, testComment(text)); err != nil {
			t.Fatalf("AddComment(%q) error = %v", text, err)
		}
	}

	if _, err := service.UpdateComment(pr.ID, 1, testComment("second updated")); err != nil {
		t.Fatalf("UpdateComment() error = %v", err)
	}
	loaded, _, err := service.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := commentTexts(loaded.Comments); strings.Join(got, ",") != "first,second updated,third" {
		t.Fatalf("comments after add and update = %v, want original order and cardinality", got)
	}
}

func TestMergePRMergesMatchingSourceHeadAndCleansUp(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)

	if _, _, err := service.MergePR(context.Background(), pr.ID, true); err != nil {
		t.Fatal(err)
	}

	if got := testGit(t, repoPath, "rev-parse", "main"); got != pr.SourceHeadSHA {
		t.Fatalf("main = %s, want source head %s", got, pr.SourceHeadSHA)
	}
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Fatalf("source worktree still exists or stat failed unexpectedly: %v", err)
	}
	if got := testGit(t, repoPath, "branch", "--list", "feature"); got != "" {
		t.Fatalf("source branch still exists: %q", got)
	}
}

func TestMergePRRefusesMovedSourceHead(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)
	testGit(t, repoPath, "tag", "feature", pr.SourceHeadSHA)
	writeTestFile(t, featurePath, "follow-up.txt", "follow-up\n")
	testGit(t, featurePath, "add", "follow-up.txt")
	testGit(t, featurePath, "commit", "-m", "follow-up")
	currentHeadSHA := testGit(t, featurePath, "rev-parse", "HEAD")

	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil {
		t.Fatal("MergePR() error = nil, want moved source head error")
	}
	for _, want := range []string{pr.SourceHeadSHA, currentHeadSHA, "reject", "create a new PR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("MergePR() error = %q, want it to contain %q", err, want)
		}
	}
	if got := testGit(t, repoPath, "rev-parse", "main"); got == pr.SourceHeadSHA {
		t.Fatalf("main advanced to reviewed source head %s after rejected merge", got)
	}
}

func TestMergePRRefusesDeletedSourceBranch(t *testing.T) {
	repoPath, featurePath, service, pr := newMergeTestPR(t)
	testGit(t, repoPath, "worktree", "remove", "--force", featurePath)
	testGit(t, repoPath, "branch", "-D", "feature")

	_, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err == nil {
		t.Fatal("MergePR() error = nil, want deleted source branch error")
	}
	for _, want := range []string{"feature", "no longer exists", "reject", "create a new PR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("MergePR() error = %q, want it to contain %q", err, want)
		}
	}
}

func TestMergePRRefusesConcurrentCloseBeforeBranchMerge(t *testing.T) {
	repoPath, _, serviceA, pr := newMergeTestPR(t)
	serviceB := secondService(t, repoPath)
	serviceA.beforeMergeHook = func() {
		serviceA.beforeMergeHook = nil
		if _, _, err := serviceB.RejectPR(pr.ID); err != nil {
			t.Errorf("concurrent RejectPR() error = %v", err)
		}
	}

	_, _, err := serviceA.MergePR(context.Background(), pr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("MergePR() error = %v, want closed-before-merge refusal", err)
	}
	if got := testGit(t, repoPath, "rev-parse", "main"); got == pr.SourceHeadSHA {
		t.Fatalf("main advanced to %s after concurrent close", got)
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

	pr := createLegacyTestPR(t, svc, repoPath, "test PR")
	return svc, pr
}

func secondService(t *testing.T, repoPath string) *Service {
	t.Helper()
	service, err := NewService(repoPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func testComment(text string) model.Comment {
	return model.Comment{
		FilePath:  "app.txt",
		LineStart: 2,
		LineEnd:   2,
		Comment:   text,
	}
}

func assertCommentTextsExactlyOnce(t *testing.T, service *Service, id string, want ...string) {
	t.Helper()
	pr, _, err := service.LoadPR(id)
	if err != nil {
		t.Fatalf("LoadPR() error = %v", err)
	}
	counts := make(map[string]int)
	for _, comment := range pr.Comments {
		counts[comment.Comment]++
	}
	for _, text := range want {
		if counts[text] != 1 {
			t.Errorf("comment %q count = %d, want 1; all comments = %v", text, counts[text], commentTexts(pr.Comments))
		}
	}
}

func newMergeTestPR(t *testing.T) (string, string, *Service, model.PR) {
	t.Helper()

	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "base.txt", "base\n")
	testGit(t, repoPath, "add", "base.txt")
	testGit(t, repoPath, "commit", "-m", "base")

	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "feature.txt", "feature\n")
	testGit(t, featurePath, "add", "feature.txt")
	testGit(t, featurePath, "commit", "-m", "feature")

	service, err := NewService(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	pr := createLegacyTestPR(t, service, featurePath, "Guard reviewed snapshot")

	return repoPath, featurePath, service, pr
}

func newMergeConflictTestPR(t *testing.T) (string, *Service, model.PR) {
	t.Helper()
	repoPath := t.TempDir()
	testGit(t, repoPath, "init", "-b", "main")
	testGit(t, repoPath, "config", "user.name", "gitpr tests")
	testGit(t, repoPath, "config", "user.email", "gitpr@example.test")
	writeTestFile(t, repoPath, "record.txt", "shared\n")
	testGit(t, repoPath, "add", "record.txt")
	testGit(t, repoPath, "commit", "-m", "base")

	featurePath := filepath.Join(t.TempDir(), "feature")
	testGit(t, repoPath, "worktree", "add", "-b", "feature", featurePath, "HEAD")
	writeTestFile(t, featurePath, "record.txt", "feature\n")
	testGit(t, featurePath, "add", "record.txt")
	testGit(t, featurePath, "commit", "-m", "feature")
	writeTestFile(t, repoPath, "record.txt", "main\n")
	testGit(t, repoPath, "add", "record.txt")
	testGit(t, repoPath, "commit", "-m", "main")

	service, err := NewService(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	pr := createLegacyTestPR(t, service, featurePath, "conflicting changes")
	return repoPath, service, pr
}

func createLegacyTestPR(t *testing.T, service *Service, worktree, title string) model.PR {
	t.Helper()
	ctx := context.Background()
	repo, branch, base, _, err := service.repoContext(ctx, worktree, "")
	if err != nil {
		t.Fatal(err)
	}
	fileDiffs, err := repo.FileDiffs(ctx, base, branch)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := repo.Commits(ctx, base, branch)
	if err != nil {
		t.Fatal(err)
	}
	sourceHead, err := repo.HeadSHA(ctx, branch)
	if err != nil {
		t.Fatal(err)
	}
	baseHead, err := repo.HeadSHA(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	mergeBase, err := repo.MergeBase(ctx, sourceHead, baseHead)
	if err != nil {
		t.Fatal(err)
	}
	conflicts, err := repo.DetectMergeConflicts(ctx, base, sourceHead)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pr := model.PR{ID: ulid.Make().String(), Title: title, SourceBranch: branch, SourceWorktreePath: repo.WorktreePath, RepositoryRoot: repo.CommonRoot, BaseBranch: base, SourceHeadSHA: sourceHead, BaseHeadSHA: baseHead, MergeBaseSHA: mergeBase, FileDiffs: fileDiffs, Commits: commits, MergeConflicts: conflicts, Status: model.StatusOpen, CreatedAt: now, UpdatedAt: now}
	if _, err := service.store.SavePR(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	return pr
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
