package app

import (
	"context"
	"errors"
	"fmt"
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

func (s *Service) CreatePR(ctx context.Context, req CreatePRRequest) (model.PR2, string, error) {
	if strings.TrimSpace(req.Title) == "" {
		return model.PR2{}, "", errors.New("title is required")
	}

	repo, branch, baseBranch, cfg, err := s.repoContext(ctx, req.Worktree, req.BaseBranch)
	if err != nil {
		return model.PR2{}, "", err
	}

	if branch == baseBranch {
		return model.PR2{}, "", fmt.Errorf("source branch %q matches base branch %q", branch, baseBranch)
	}

	open, _, err := s.store.ListPRs(string(model.PRStateOpen))
	if err != nil {
		return model.PR2{}, "", err
	}
	for _, pr := range open {
		if pr.SourceBranch == branch && pr.BaseBranch == baseBranch {
			return model.PR2{}, "", fmt.Errorf("open branch-based PR %s already tracks %s into %s", pr.ID, branch, baseBranch)
		}
	}

	now := time.Now().UTC()
	pr := model.PR2{
		Schema:             2,
		ID:                 ulid.Make().String(),
		Title:              strings.TrimSpace(req.Title),
		SourceBranch:       branch,
		SourceWorktreePath: repo.WorktreePath,
		RepositoryRoot:     repo.CommonRoot,
		BaseBranch:         baseBranch,
		Description:        req.Description,
		State:              model.PRStateOpen,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if cfg.DefaultBranch != baseBranch {
		cfg.DefaultBranch = baseBranch
		if err := s.store.SaveConfig(cfg); err != nil {
			return model.PR2{}, "", err
		}
	}

	ref, err := s.store.SavePR2(pr, "", "")
	if err != nil {
		if errors.Is(err, store.ErrOpenPairConflict) {
			return model.PR2{}, "", fmt.Errorf("open branch-based PR already tracks %s into %s: %w", branch, baseBranch, err)
		}
		return model.PR2{}, "", err
	}

	return pr, ref, nil
}

func (s *Service) ListPRs(status string) ([]model.PR2, int, error) {
	return s.store.ListPRs(status)
}

func (s *Service) LoadRecord(id string) (model.PR2, string, error) { return s.store.LoadPR(id) }

func (s *Service) DebugExport(id, targetDir string) error {
	return s.store.ExportPR(id, targetDir)
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

// SkippedRecordsMessage reports records that listing could not read: legacy
// snapshots and records written by a newer gitpr. It names the documented
// raw-git path so a legacy-record holder can act without another tool.
func SkippedRecordsMessage(skipped int) string {
	return fmt.Sprintf(
		"Skipped %d unreadable record(s) (legacy or newer schema). "+
			"Legacy records are read and removed with raw git; see the \"Legacy records\" section of the gitpr README and docs/usage.md.",
		skipped,
	)
}
