package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

type Service struct {
	store *store.Store
}

type CreatePRRequest struct {
	Title       string
	Description string
	Worktree    string
	BaseBranch  string
}

func NewService(root string) (*Service, error) {
	st, err := store.New(root)
	if err != nil {
		return nil, err
	}
	return &Service{store: st}, nil
}

func (s *Service) CreatePR(ctx context.Context, req CreatePRRequest) (model.PR, string, error) {
	if strings.TrimSpace(req.Title) == "" {
		return model.PR{}, "", errors.New("title is required")
	}

	repo, branch, baseBranch, cfg, err := s.repoContext(ctx, req.Worktree, req.BaseBranch)
	if err != nil {
		return model.PR{}, "", err
	}

	if branch == baseBranch {
		return model.PR{}, "", fmt.Errorf("source branch %q matches base branch %q", branch, baseBranch)
	}

	fileDiffs, err := repo.FileDiffs(ctx, baseBranch, branch)
	if err != nil {
		return model.PR{}, "", err
	}
	if len(fileDiffs) == 0 {
		return model.PR{}, "", errors.New("no diff detected between source branch and base branch")
	}

	commits, err := repo.Commits(ctx, baseBranch, branch)
	if err != nil {
		return model.PR{}, "", err
	}

	sourceHeadSHA, err := repo.HeadSHA(ctx, branch)
	if err != nil {
		return model.PR{}, "", err
	}
	baseHeadSHA, err := repo.HeadSHA(ctx, baseBranch)
	if err != nil {
		return model.PR{}, "", err
	}
	mergeBaseSHA, err := repo.MergeBase(ctx, sourceHeadSHA, baseHeadSHA)
	if err != nil {
		return model.PR{}, "", err
	}

	conflicts, err := repo.DetectMergeConflicts(ctx, baseBranch, sourceHeadSHA)
	if err != nil {
		return model.PR{}, "", err
	}

	now := time.Now().UTC()
	pr := model.PR{
		ID:                 ulid.Make().String(),
		Title:              strings.TrimSpace(req.Title),
		SourceBranch:       branch,
		SourceWorktreePath: repo.WorktreePath,
		RepositoryRoot:     repo.CommonRoot,
		BaseBranch:         baseBranch,
		SourceHeadSHA:      sourceHeadSHA,
		BaseHeadSHA:        baseHeadSHA,
		MergeBaseSHA:       mergeBaseSHA,
		Description:        strings.TrimSpace(req.Description),
		FileDiffs:          fileDiffs,
		Commits:            commits,
		MergeConflicts:     conflicts,
		Status:             model.StatusOpen,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	ref, err := s.store.SavePR(pr, "")
	if err != nil {
		return model.PR{}, "", err
	}

	cfg.DefaultBranch = baseBranch
	if err := s.store.SaveConfig(cfg); err != nil {
		return model.PR{}, "", err
	}

	return pr, ref, nil
}

func (s *Service) ListPRs(status string) ([]model.PR, error) {
	return s.store.ListPRs(status)
}

func (s *Service) LoadPR(id string) (model.PR, string, error) {
	return s.store.LoadPR(id)
}

func (s *Service) RefreshConflicts(ctx context.Context, pr model.PR) (model.PR, error) {
	if pr.Status != model.StatusOpen {
		return pr, nil
	}

	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return pr, err
	}

	conflicts, err := repo.DetectMergeConflicts(ctx, pr.BaseBranch, pr.SourceHeadSHA)
	if err != nil {
		return pr, err
	}

	pr.MergeConflicts = conflicts
	pr.UpdatedAt = time.Now().UTC()
	_, err = s.store.SavePR(pr, pr.Status)
	return pr, err
}

func (s *Service) AddComment(id string, comment model.Comment) (model.PR, error) {
	pr, _, err := s.store.LoadPR(id)
	if err != nil {
		return model.PR{}, err
	}

	if pr.Status != model.StatusOpen {
		return model.PR{}, errors.New("cannot comment on a closed PR")
	}

	comment.Comment = strings.TrimSpace(comment.Comment)
	if comment.Comment == "" {
		return model.PR{}, errors.New("comment text is required")
	}

	updated := false
	for idx, existing := range pr.Comments {
		if existing.FilePath == comment.FilePath && existing.LineStart == comment.LineStart && existing.LineEnd == comment.LineEnd {
			comment.CreatedAt = existing.CreatedAt
			if comment.CommitSHA == "" {
				comment.CommitSHA = existing.CommitSHA
			}
			pr.Comments[idx] = comment
			updated = true
			break
		}
	}
	if !updated {
		comment.CreatedAt = time.Now().UTC()
		pr.Comments = append(pr.Comments, comment)
	}

	pr.UpdatedAt = time.Now().UTC()
	if _, err := s.store.SavePR(pr, pr.Status); err != nil {
		return model.PR{}, err
	}

	return pr, nil
}

func (s *Service) RejectPR(id string) (model.PR, string, error) {
	pr, _, err := s.store.LoadPR(id)
	if err != nil {
		return model.PR{}, "", err
	}

	if pr.Status != model.StatusOpen {
		return model.PR{}, "", fmt.Errorf("PR %s is already closed", pr.ID)
	}

	previousStatus := pr.Status
	now := time.Now().UTC()
	pr.Status = model.StatusRejected
	pr.UpdatedAt = now
	pr.ClosedAt = &now
	ref, err := s.store.SavePR(pr, previousStatus)
	if err != nil {
		return model.PR{}, "", err
	}

	return pr, ref, nil
}

func (s *Service) MergePR(ctx context.Context, id string, cleanup bool) (model.PR, string, error) {
	pr, _, err := s.store.LoadPR(id)
	if err != nil {
		return model.PR{}, "", err
	}

	if pr.Status != model.StatusOpen {
		return model.PR{}, "", fmt.Errorf("PR %s is already closed", pr.ID)
	}

	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return model.PR{}, "", err
	}

	conflicts, err := repo.DetectMergeConflicts(ctx, pr.BaseBranch, pr.SourceHeadSHA)
	if err != nil {
		return model.PR{}, "", err
	}
	pr.MergeConflicts = conflicts

	if len(conflicts) > 0 {
		pr.UpdatedAt = time.Now().UTC()
		if _, err := s.store.SavePR(pr, pr.Status); err != nil {
			return model.PR{}, "", err
		}
		return model.PR{}, "", errors.New("merge conflicts detected; PR cannot be merged")
	}

	if err := repo.MergeBranch(ctx, pr.BaseBranch, pr.SourceHeadSHA); err != nil {
		return model.PR{}, "", err
	}

	var cleanupErr error
	if cleanup {
		cleanupErr = repo.CleanupSourceWorktree(ctx, pr.SourceWorktreePath, pr.SourceBranch)
	}

	previousStatus := pr.Status
	now := time.Now().UTC()
	pr.Status = model.StatusApproved
	pr.UpdatedAt = now
	pr.ClosedAt = &now
	ref, err := s.store.SavePR(pr, previousStatus)
	if err != nil {
		return model.PR{}, "", err
	}

	if cleanupErr != nil {
		return pr, ref, fmt.Errorf("merged successfully, but cleanup failed: %w", cleanupErr)
	}

	return pr, ref, nil
}

func (s *Service) OpenPRs() ([]model.PR, error) {
	prs, err := s.store.ListPRs(string(model.StatusOpen))
	if err != nil {
		return nil, err
	}

	sort.Slice(prs, func(i, j int) bool {
		return prs[i].ID < prs[j].ID
	})
	return prs, nil
}

func (s *Service) DebugExport(id, which, targetDir string) error {
	return s.store.ExportPR(id, which, targetDir)
}

func (s *Service) repoContext(ctx context.Context, worktree, baseOverride string) (*gitutil.Repo, string, string, model.Config, error) {
	cfg, err := s.store.LoadConfig()
	if err != nil {
		return nil, "", "", model.Config{}, err
	}

	repo, err := gitutil.Open(worktree)
	if err != nil {
		return nil, "", "", model.Config{}, err
	}

	branch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return nil, "", "", model.Config{}, err
	}

	baseBranch, err := repo.DetectDefaultBranch(ctx, firstNonEmpty(baseOverride, cfg.DefaultBranch))
	if err != nil {
		return nil, "", "", model.Config{}, err
	}

	return repo, branch, baseBranch, cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
