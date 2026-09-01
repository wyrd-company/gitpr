package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func (s *Service) MergeRecord(ctx context.Context, id string, cleanup bool) (model.Record, string, error) {
	record, _, err := s.store.LoadPR(id)
	if err != nil {
		return nil, "", err
	}
	if _, legacy := record.(model.PR); legacy {
		pr, ref, err := s.MergePR(ctx, id, cleanup)
		return pr, ref, err
	}
	pr, ref, err := s.mergeBranchPR(ctx, id, cleanup)
	return pr, ref, err
}

func (s *Service) mergeBranchPR(ctx context.Context, id string, cleanup bool) (model.PR2, string, error) {
	pr, version, err := s.store.LoadPR2(id)
	if err != nil {
		return model.PR2{}, "", err
	}
	if pr.State != model.PRStateOpen {
		return model.PR2{}, "", fmt.Errorf("cannot merge PR %s in %s state; only open branch-based PRs can merge", pr.ID, pr.State)
	}
	if len(pr.Events) == 0 {
		return model.PR2{}, "", fmt.Errorf("PR %s has no review event; run gitpr review and record an accepted event before merging", pr.ID)
	}
	latest := pr.Events[len(pr.Events)-1]
	if latest.Verdict != model.VerdictAccepted {
		return model.PR2{}, "", fmt.Errorf("latest review event %s is rejected; review the current basis and record a new accepted event before merging", latest.ID)
	}
	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return model.PR2{}, "", err
	}
	sourceExists, err := repo.BranchExists(ctx, pr.SourceBranch)
	if err != nil {
		return model.PR2{}, "", err
	}
	if !sourceExists {
		return model.PR2{}, "", fmt.Errorf("source branch %q is deleted: recorded head %s, live identity is deleted", pr.SourceBranch, latest.SourceHeadSHA)
	}
	liveSource, err := repo.HeadSHA(ctx, "refs/heads/"+pr.SourceBranch)
	if err != nil {
		return model.PR2{}, "", err
	}
	if liveSource != latest.SourceHeadSHA {
		return model.PR2{}, "", fmt.Errorf("source branch %q drifted: recorded head %s, live head %s", pr.SourceBranch, latest.SourceHeadSHA, liveSource)
	}
	baseExists, err := repo.BranchExists(ctx, pr.BaseBranch)
	if err != nil {
		return model.PR2{}, "", err
	}
	if !baseExists {
		return model.PR2{}, "", fmt.Errorf("base branch %q is deleted: recorded head %s, live identity is deleted", pr.BaseBranch, latest.BaseHeadSHA)
	}
	liveBase, err := repo.HeadSHA(ctx, "refs/heads/"+pr.BaseBranch)
	if err != nil {
		return model.PR2{}, "", err
	}
	if liveBase != latest.BaseHeadSHA {
		return model.PR2{}, "", fmt.Errorf("base branch %q drifted: recorded head %s, live head %s", pr.BaseBranch, latest.BaseHeadSHA, liveBase)
	}
	if latest.BaseHeadSHA == latest.SourceHeadSHA {
		return model.PR2{}, "", fmt.Errorf("review event %s is not a strict fast-forward: recorded base and source are both %s", latest.ID, latest.BaseHeadSHA)
	}
	ancestor, err := repo.IsAncestor(ctx, latest.BaseHeadSHA, latest.SourceHeadSHA)
	if err != nil {
		return model.PR2{}, "", err
	}
	if !ancestor {
		return model.PR2{}, "", fmt.Errorf("review event %s is not a fast-forward: recorded base %s is not an ancestor of recorded source %s", latest.ID, latest.BaseHeadSHA, latest.SourceHeadSHA)
	}
	baseWorktree, err := repo.PrepareBranchRefUpdate(ctx, pr.BaseBranch)
	if err != nil {
		return model.PR2{}, "", err
	}
	now := time.Now().UTC()
	pr.State = model.PRStateMerged
	pr.MergedAt = &now
	pr.MergedEventID = latest.ID
	pr.UpdatedAt = now
	ref, err := s.store.MergePR2(pr, version)
	if err != nil {
		if errors.Is(err, store.ErrMergeConflict) {
			return model.PR2{}, "", fmt.Errorf("%w; a branch or PR record moved during merge, so review the live basis and retry the flow", err)
		}
		return model.PR2{}, "", err
	}
	if baseWorktree != "" {
		if err := repo.RefreshWorktree(ctx, baseWorktree, latest.SourceHeadSHA); err != nil {
			return pr, ref, fmt.Errorf("merge succeeded, but worktree refresh failed for %s; repair with git -C %s reset --hard %s: %w", baseWorktree, baseWorktree, latest.SourceHeadSHA, err)
		}
	}
	if cleanup {
		if err := repo.CleanupSourceWorktree(ctx, pr.SourceWorktreePath, pr.SourceBranch); err != nil {
			return pr, ref, fmt.Errorf("merge succeeded, but cleanup failed for source worktree %s; repair by removing that worktree and deleting branch %s: %w", pr.SourceWorktreePath, pr.SourceBranch, err)
		}
	}
	return pr, ref, nil
}
