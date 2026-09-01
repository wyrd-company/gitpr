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
	store           *store.Store
	beforeMergeHook func()
}

const metadataMutationAttempts = 5

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

	ref, err := s.store.SavePR(pr, "", "")
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
	return s.mutatePR(pr.ID, func(current *model.PR) error {
		if current.Status != model.StatusOpen {
			return fmt.Errorf("PR %s is already closed", current.ID)
		}

		repo, err := gitutil.Open(current.RepositoryRoot)
		if err != nil {
			return err
		}
		conflicts, err := repo.DetectMergeConflicts(ctx, current.BaseBranch, current.SourceHeadSHA)
		if err != nil {
			return err
		}
		current.MergeConflicts = conflicts
		current.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *Service) AddComment(id string, comment model.Comment) (model.PR, error) {
	comment.Comment = strings.TrimSpace(comment.Comment)
	if comment.Comment == "" {
		return model.PR{}, errors.New("comment text is required")
	}
	comment.CreatedAt = time.Now().UTC()
	return s.mutatePR(id, func(pr *model.PR) error {
		if pr.Status != model.StatusOpen {
			return errors.New("cannot comment on a closed PR")
		}
		pr.Comments = append(pr.Comments, comment)
		pr.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *Service) UpdateComment(id string, commentIndex int, comment model.Comment) (model.PR, error) {
	comment.Comment = strings.TrimSpace(comment.Comment)
	if comment.Comment == "" {
		return model.PR{}, errors.New("comment text is required")
	}
	input := comment
	return s.mutatePR(id, func(pr *model.PR) error {
		replacement := input
		if pr.Status != model.StatusOpen {
			return errors.New("cannot comment on a closed PR")
		}
		if commentIndex < 0 || commentIndex >= len(pr.Comments) {
			return fmt.Errorf("comment index %d is out of range", commentIndex)
		}

		existing := pr.Comments[commentIndex]
		if existing.FilePath != replacement.FilePath || existing.LineStart != replacement.LineStart || existing.LineEnd != replacement.LineEnd {
			return fmt.Errorf(
				"comment anchor mismatch: index %d is anchored at %s:%d-%d, not %s:%d-%d",
				commentIndex,
				existing.FilePath, existing.LineStart, existing.LineEnd,
				replacement.FilePath, replacement.LineStart, replacement.LineEnd,
			)
		}

		replacement.CreatedAt = existing.CreatedAt
		if replacement.CommitSHA == "" {
			replacement.CommitSHA = existing.CommitSHA
		}
		pr.Comments[commentIndex] = replacement
		pr.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *Service) RejectPR(id string) (model.PR, string, error) {
	pr, ref, err := s.mutatePRRef(id, func(pr *model.PR) error {
		if pr.Status != model.StatusOpen {
			return fmt.Errorf("PR %s is already closed", pr.ID)
		}
		now := time.Now().UTC()
		pr.Status = model.StatusRejected
		pr.UpdatedAt = now
		pr.ClosedAt = &now
		return nil
	})
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

	sourceBranchExists, err := repo.BranchExists(ctx, pr.SourceBranch)
	if err != nil {
		return model.PR{}, "", fmt.Errorf("check source branch %q: %w", pr.SourceBranch, err)
	}
	if !sourceBranchExists {
		return model.PR{}, "", fmt.Errorf(
			"source branch %q no longer exists; reject PR %s and create a new PR from the current head",
			pr.SourceBranch,
			pr.ID,
		)
	}

	currentSourceHeadSHA, err := repo.HeadSHA(ctx, "refs/heads/"+pr.SourceBranch)
	if err != nil {
		return model.PR{}, "", fmt.Errorf("resolve source branch %q: %w", pr.SourceBranch, err)
	}
	if currentSourceHeadSHA != pr.SourceHeadSHA {
		return model.PR{}, "", fmt.Errorf(
			"source branch %q moved from reviewed snapshot %s to current head %s; reject PR %s and create a new PR from the current head",
			pr.SourceBranch,
			pr.SourceHeadSHA,
			currentSourceHeadSHA,
			pr.ID,
		)
	}

	conflicts, err := repo.DetectMergeConflicts(ctx, pr.BaseBranch, pr.SourceHeadSHA)
	if err != nil {
		return model.PR{}, "", err
	}
	pr.MergeConflicts = conflicts

	if len(conflicts) > 0 {
		if _, err := s.mutatePR(pr.ID, func(current *model.PR) error {
			if current.Status != model.StatusOpen {
				return fmt.Errorf("PR %s is already closed", current.ID)
			}
			currentRepo, err := gitutil.Open(current.RepositoryRoot)
			if err != nil {
				return err
			}
			currentConflicts, err := currentRepo.DetectMergeConflicts(ctx, current.BaseBranch, current.SourceHeadSHA)
			if err != nil {
				return err
			}
			current.MergeConflicts = currentConflicts
			current.UpdatedAt = time.Now().UTC()
			return nil
		}); err != nil {
			return model.PR{}, "", err
		}
		return model.PR{}, "", errors.New("merge conflicts detected; PR cannot be merged")
	}
	if s.beforeMergeHook != nil {
		s.beforeMergeHook()
	}
	current, _, err := s.store.LoadPR(pr.ID)
	if err != nil {
		return model.PR{}, "", err
	}
	if current.Status != model.StatusOpen {
		return model.PR{}, "", fmt.Errorf("PR %s is already closed", current.ID)
	}

	if err := repo.MergeBranch(ctx, pr.BaseBranch, pr.SourceHeadSHA); err != nil {
		return model.PR{}, "", err
	}

	var cleanupErr error
	if cleanup {
		cleanupErr = repo.CleanupSourceWorktree(ctx, pr.SourceWorktreePath, pr.SourceBranch)
	}

	mergedPR := pr
	updatedPR, ref, err := s.mutatePRRef(pr.ID, func(current *model.PR) error {
		if current.Status != model.StatusOpen {
			return fmt.Errorf("PR %s is already closed", current.ID)
		}
		now := time.Now().UTC()
		current.Status = model.StatusApproved
		current.UpdatedAt = now
		current.ClosedAt = &now
		return nil
	})
	if err != nil {
		return mergedPR, "", fmt.Errorf(
			"merge succeeded for PR %s at %s, but its metadata record needs repair; retry the command to record the merge",
			mergedPR.ID,
			mergedPR.SourceHeadSHA,
		)
	}
	if cleanupErr != nil {
		return updatedPR, ref, fmt.Errorf("merged successfully, but cleanup failed: %w", cleanupErr)
	}

	return updatedPR, ref, nil
}

func (s *Service) mutatePR(id string, mutate func(*model.PR) error) (model.PR, error) {
	pr, _, err := s.mutatePRRef(id, mutate)
	return pr, err
}

func (s *Service) mutatePRRef(id string, mutate func(*model.PR) error) (model.PR, string, error) {
	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR(id)
		if err != nil {
			return model.PR{}, "", err
		}
		previousStatus := pr.Status
		if err := mutate(&pr); err != nil {
			return model.PR{}, "", err
		}
		if ref, err := s.store.SavePR(pr, previousStatus, version); err == nil {
			return pr, ref, nil
		} else if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR{}, "", err
		}
	}
	return model.PR{}, "", fmt.Errorf("%w after %d attempts; retry the command", store.ErrMetadataConflict, metadataMutationAttempts)
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
