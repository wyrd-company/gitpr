package gitutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	diffpkg "github.com/sourcegraph/go-diff/diff"

	"github.com/wyrd-company/gitpr/internal/model"
)

type Repo struct {
	WorktreePath string
	RepoRoot     string
	CommonRoot   string
}

func Open(path string) (*Repo, error) {
	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	root, err := runGit(context.Background(), absPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("determine repo root: %w", err)
	}

	commonDir, err := runGit(context.Background(), absPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("determine git common dir: %w", err)
	}
	commonDir = strings.TrimSpace(commonDir)
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(strings.TrimSpace(root), commonDir)
	}

	commonRoot := commonDir
	if filepath.Base(commonDir) == ".git" {
		commonRoot = filepath.Dir(commonDir)
	}

	return &Repo{
		WorktreePath: absPath,
		RepoRoot:     strings.TrimSpace(root),
		CommonRoot:   filepath.Clean(commonRoot),
	}, nil
}

func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := runGit(ctx, r.WorktreePath, "branch", "--show-current")
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(out)
	if branch == "" {
		return "", errors.New("current worktree is not on a branch")
	}
	return branch, nil
}

func (r *Repo) HeadSHA(ctx context.Context, ref string) (string, error) {
	out, err := runGit(ctx, r.WorktreePath, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) MergeBase(ctx context.Context, leftRef, rightRef string) (string, error) {
	out, err := runGit(ctx, r.WorktreePath, "merge-base", leftRef, rightRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) FileContentAtRef(ctx context.Context, ref, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", os.ErrNotExist
	}

	out, err := runGit(ctx, r.CommonRoot, "show", ref+":"+path)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "invalid object name") || strings.Contains(msg, "exists on disk, but not in") {
			return "", os.ErrNotExist
		}
		return "", err
	}
	return out, nil
}

func (r *Repo) DetectDefaultBranch(ctx context.Context, configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured), nil
	}

	candidates := []func() (string, error){
		func() (string, error) {
			out, err := runGit(ctx, r.WorktreePath, "symbolic-ref", "refs/remotes/origin/HEAD")
			if err != nil {
				return "", err
			}
			ref := strings.TrimSpace(out)
			return filepath.Base(ref), nil
		},
		func() (string, error) {
			if ok, _ := r.BranchExists(ctx, "main"); ok {
				return "main", nil
			}
			return "", errors.New("main not found")
		},
		func() (string, error) {
			if ok, _ := r.BranchExists(ctx, "master"); ok {
				return "master", nil
			}
			return "", errors.New("master not found")
		},
	}

	for _, candidate := range candidates {
		branch, err := candidate()
		if err == nil && branch != "" {
			return branch, nil
		}
	}

	return "", errors.New("unable to detect default branch; pass --base to create")
}

func (r *Repo) BranchExists(ctx context.Context, branch string) (bool, error) {
	_, err := runGit(ctx, r.WorktreePath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	return false, err
}

func (r *Repo) Commits(ctx context.Context, baseBranch, sourceBranch string) ([]model.Commit, error) {
	out, err := runGit(ctx, r.WorktreePath, "log", "--format=%H%x1f%s", baseBranch+".."+sourceBranch)
	if err != nil {
		return nil, err
	}

	lines := splitNonEmpty(out)
	commits := make([]model.Commit, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\x1f", 2)
		if len(parts) != 2 {
			continue
		}

		commits = append(commits, model.Commit{
			SHA:     parts[0],
			Message: parts[1],
		})
	}

	return commits, nil
}

func (r *Repo) FileDiffs(ctx context.Context, baseBranch, sourceBranch string) ([]model.FileDiff, error) {
	patch, err := runGit(ctx, r.WorktreePath, "diff", "--find-renames", "--unified=3", baseBranch+"..."+sourceBranch)
	if err != nil {
		return nil, err
	}

	patch = strings.TrimSpace(patch)
	if patch == "" {
		return nil, nil
	}

	multi, err := diffpkg.ParseMultiFileDiff([]byte(patch))
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}

	fileDiffs := make([]model.FileDiff, 0, len(multi))
	for _, fd := range multi {
		if fd == nil {
			continue
		}

		fileDiffs = append(fileDiffs, convertFileDiff(fd))
	}

	return fileDiffs, nil
}

func (r *Repo) DetectMergeConflicts(ctx context.Context, baseBranch, sourceBranch string) ([]model.MergeConflict, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", r.WorktreePath, "merge-tree", "--write-tree", "--messages", baseBranch, sourceBranch)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	conflicts := parseMergeConflicts(output)
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	if err != nil {
		return nil, fmt.Errorf("merge-tree: %w", err)
	}

	return nil, nil
}

func (r *Repo) MergeBranch(ctx context.Context, baseBranch, sourceRef string) error {
	baseWorktrees, err := r.worktreesForBranch(ctx, baseBranch)
	if err != nil {
		return fmt.Errorf("find worktrees for %s: %w", baseBranch, err)
	}
	if len(baseWorktrees) > 1 {
		return fmt.Errorf(
			"base branch %s is checked out in multiple worktrees; detach it in all but one before merging so gitpr can synchronize it safely",
			baseBranch,
		)
	}
	if len(baseWorktrees) == 1 {
		if _, err := runGit(ctx, baseWorktrees[0], "merge", "--ff-only", sourceRef); err != nil {
			return fmt.Errorf("merge branch in checked-out base worktree %s: %w", baseWorktrees[0], err)
		}
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "gitpr-merge-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if _, err := runGit(ctx, r.WorktreePath, "worktree", "add", "--detach", tmpDir, baseBranch); err != nil {
		return fmt.Errorf("create temporary merge worktree: %w", err)
	}
	defer func() {
		_, _ = runGit(context.Background(), r.WorktreePath, "worktree", "remove", "--force", tmpDir)
	}()

	if _, err := runGit(ctx, tmpDir, "merge", "--ff-only", sourceRef); err != nil {
		return fmt.Errorf("merge branch: %w", err)
	}

	if _, err := runGit(ctx, tmpDir, "update-ref", "refs/heads/"+baseBranch, "HEAD"); err != nil {
		return fmt.Errorf("update %s reference: %w", baseBranch, err)
	}

	return nil
}

func (r *Repo) worktreesForBranch(ctx context.Context, branch string) ([]string, error) {
	out, err := runGit(ctx, r.WorktreePath, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	targetRef := "refs/heads/" + branch
	var worktrees []string
	for _, record := range strings.Split(out, "\x00\x00") {
		var path string
		var branchRef string
		for _, field := range strings.Split(record, "\x00") {
			switch {
			case strings.HasPrefix(field, "worktree "):
				path = strings.TrimPrefix(field, "worktree ")
			case strings.HasPrefix(field, "branch "):
				branchRef = strings.TrimPrefix(field, "branch ")
			}
		}
		if path != "" && branchRef == targetRef {
			worktrees = append(worktrees, path)
		}
	}

	return worktrees, nil
}

func (r *Repo) CleanupSourceWorktree(ctx context.Context, sourceWorktreePath, sourceBranch string) error {
	currentWD, err := os.Getwd()
	if err == nil {
		currentWD, _ = filepath.Abs(currentWD)
		sourceWorktreePath, _ = filepath.Abs(sourceWorktreePath)
		if currentWD == sourceWorktreePath || strings.HasPrefix(currentWD, sourceWorktreePath+string(filepath.Separator)) {
			return fmt.Errorf("cannot remove current working directory worktree %s", sourceWorktreePath)
		}
	}

	if _, err := runGit(ctx, r.WorktreePath, "worktree", "remove", "--force", sourceWorktreePath); err != nil {
		return err
	}

	if sourceBranch != "" {
		if _, err := runGit(ctx, r.WorktreePath, "branch", "-D", sourceBranch); err != nil {
			return err
		}
	}

	return nil
}

func convertFileDiff(fd *diffpkg.FileDiff) model.FileDiff {
	result := model.FileDiff{
		OldPath: normalizeDiffPath(fd.OrigName),
		NewPath: normalizeDiffPath(fd.NewName),
		Status:  fileStatus(fd),
		Patch:   rawFilePatch(fd),
	}

	result.Hunks = make([]model.Hunk, 0, len(fd.Hunks))
	for _, hunk := range fd.Hunks {
		if hunk == nil {
			continue
		}
		result.Hunks = append(result.Hunks, convertHunk(hunk))
	}

	return result
}

func convertHunk(hunk *diffpkg.Hunk) model.Hunk {
	result := model.Hunk{
		Header:   strings.TrimSpace(hunk.Section),
		OldStart: int(hunk.OrigStartLine),
		OldLines: int(hunk.OrigLines),
		NewStart: int(hunk.NewStartLine),
		NewLines: int(hunk.NewLines),
	}

	oldLine := result.OldStart
	newLine := result.NewStart

	lines := strings.Split(strings.TrimRight(string(hunk.Body), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		prefix := line[0]
		body := line[1:]

		switch prefix {
		case ' ':
			result.Lines = append(result.Lines, model.DiffLine{
				Kind:    "context",
				OldLine: oldLine,
				NewLine: newLine,
				Content: body,
			})
			oldLine++
			newLine++
		case '-':
			result.Lines = append(result.Lines, model.DiffLine{
				Kind:    "delete",
				OldLine: oldLine,
				Content: body,
			})
			oldLine++
		case '+':
			result.Lines = append(result.Lines, model.DiffLine{
				Kind:    "add",
				NewLine: newLine,
				Content: body,
			})
			newLine++
		case '\\':
			continue
		}
	}

	return result
}

func rawFilePatch(fd *diffpkg.FileDiff) string {
	var builder strings.Builder
	oldPath := normalizeDiffPath(fd.OrigName)
	newPath := normalizeDiffPath(fd.NewName)
	builder.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", oldPath, newPath))
	builder.WriteString(fmt.Sprintf("--- %s\n", displayDiffPath("a/", oldPath)))
	builder.WriteString(fmt.Sprintf("+++ %s\n", displayDiffPath("b/", newPath)))
	for _, h := range fd.Hunks {
		builder.WriteString(formatHunkHeader(h))
		builder.WriteString(string(h.Body))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func formatHunkHeader(h *diffpkg.Hunk) string {
	header := fmt.Sprintf("@@ -%s +%s @@", formatHunkRange(int(h.OrigStartLine), int(h.OrigLines)), formatHunkRange(int(h.NewStartLine), int(h.NewLines)))
	if strings.TrimSpace(h.Section) != "" {
		header += " " + strings.TrimSpace(h.Section)
	}
	return header + "\n"
}

func formatHunkRange(start, lines int) string {
	switch lines {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start)
	default:
		return fmt.Sprintf("%d,%d", start, lines)
	}
}

func fileStatus(fd *diffpkg.FileDiff) string {
	oldPath := normalizeDiffPath(fd.OrigName)
	newPath := normalizeDiffPath(fd.NewName)

	switch {
	case oldPath == "" && newPath != "":
		return "added"
	case oldPath != "" && newPath == "":
		return "deleted"
	case oldPath != "" && newPath != "" && oldPath != newPath:
		return "renamed"
	default:
		return "modified"
	}
}

func normalizeDiffPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	if path == "/dev/null" {
		return ""
	}
	return path
}

func displayDiffPath(prefix, path string) string {
	if path == "" {
		return "/dev/null"
	}
	return prefix + path
}

func parseMergeConflicts(output string) []model.MergeConflict {
	lines := splitNonEmpty(output)
	if len(lines) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var conflicts []model.MergeConflict
	for _, line := range lines {
		if !strings.Contains(line, "CONFLICT") {
			continue
		}

		conflict := model.MergeConflict{Message: line}
		if idx := strings.LastIndex(line, " in "); idx >= 0 {
			conflict.Path = strings.TrimSpace(line[idx+4:])
		}

		key := conflict.Path + "|" + conflict.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		conflicts = append(conflicts, conflict)
	}

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Path == conflicts[j].Path {
			return conflicts[i].Message < conflicts[j].Message
		}
		return conflicts[i].Path < conflicts[j].Path
	})

	return conflicts
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitWithEnv(ctx, dir, os.Environ(), args...)
}

func runGitWithEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	}

	return stdout.String(), nil
}

func splitNonEmpty(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
