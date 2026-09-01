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

type ExpectedHeads struct {
	Source string
	Base   string
}

func (h ExpectedHeads) validate() error {
	if strings.TrimSpace(h.Source) == "" || strings.TrimSpace(h.Base) == "" {
		return errors.New("source and base heads are required; run gitpr review <id>, then pass its basis with --source-head and --base-head or --basis")
	}
	return nil
}

func (s *Service) ApprovePR(ctx context.Context, id string, heads ExpectedHeads) (model.PR2, string, error) {
	record, _, err := s.store.LoadPR(id)
	if err != nil {
		return model.PR2{}, "", err
	}
	if _, legacy := record.(model.PR); legacy {
		return model.PR2{}, "", errors.New("approve is available only for branch-based PRs; legacy PRs use merge to record their approved snapshot")
	}
	if err := heads.validate(); err != nil {
		return model.PR2{}, "", err
	}
	return s.appendVerdict(ctx, id, heads, model.VerdictAccepted, true)
}

func (s *Service) RejectRecord(ctx context.Context, id string, heads *ExpectedHeads) (model.Record, string, error) {
	record, _, err := s.store.LoadPR(id)
	if err != nil {
		return nil, "", err
	}
	if _, legacy := record.(model.PR); legacy {
		pr, ref, err := s.rejectLegacyPR(id)
		return pr, ref, err
	}
	if heads == nil {
		return nil, "", ExpectedHeads{}.validate()
	}
	pr, ref, err := s.appendVerdict(ctx, id, *heads, model.VerdictRejected, false)
	return pr, ref, err
}

func (s *Service) appendVerdict(ctx context.Context, id string, heads ExpectedHeads, verdict model.ReviewVerdict, checkLive bool) (model.PR2, string, error) {
	if err := heads.validate(); err != nil {
		return model.PR2{}, "", err
	}
	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR2(id)
		if err != nil {
			return model.PR2{}, "", err
		}
		if pr.State != model.PRStateOpen {
			return model.PR2{}, "", fmt.Errorf("cannot record a verdict on PR %s in %s state; verdicts append only while open", pr.ID, pr.State)
		}
		repo, err := gitutil.Open(pr.RepositoryRoot)
		if err != nil {
			return model.PR2{}, "", err
		}
		if checkLive {
			if err := validateLiveHeads(ctx, repo, pr, heads); err != nil {
				return model.PR2{}, "", err
			}
		} else {
			if !repo.CommitExists(ctx, heads.Source) {
				return model.PR2{}, "", fmt.Errorf("expected source head %s does not resolve to a commit", heads.Source)
			}
			if !repo.CommitExists(ctx, heads.Base) {
				return model.PR2{}, "", fmt.Errorf("expected base head %s does not resolve to a commit", heads.Base)
			}
		}
		mergeBase, err := repo.MergeBase(ctx, heads.Base, heads.Source)
		if err != nil {
			return model.PR2{}, "", fmt.Errorf("compute merge base for expected heads: %w", err)
		}
		predecessor := ""
		if len(pr.Events) > 0 {
			predecessor = pr.Events[len(pr.Events)-1].ID
		}
		pr.Events = append(pr.Events, model.ReviewEvent{
			ID: ulid.Make().String(), SourceHeadSHA: heads.Source, BaseHeadSHA: heads.Base,
			MergeBaseSHA: mergeBase, Verdict: verdict, Timestamp: time.Now().UTC(), PredecessorEventID: predecessor,
		})
		pr.UpdatedAt = time.Now().UTC()
		ref, err := s.store.SavePR2(pr, pr.State, version)
		if err == nil {
			return pr, ref, nil
		}
		if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR2{}, "", err
		}
	}
	return model.PR2{}, "", fmt.Errorf("%w after %d attempts; retry the command", store.ErrMetadataConflict, metadataMutationAttempts)
}

func validateLiveHeads(ctx context.Context, repo *gitutil.Repo, pr model.PR2, expected ExpectedHeads) error {
	sourceExists, err := repo.BranchExists(ctx, pr.SourceBranch)
	if err != nil {
		return err
	}
	if !sourceExists {
		return fmt.Errorf("source branch %q is missing: expected head %s, live identity is deleted", pr.SourceBranch, expected.Source)
	}
	liveSource, err := repo.HeadSHA(ctx, "refs/heads/"+pr.SourceBranch)
	if err != nil {
		return err
	}
	if liveSource != expected.Source {
		return fmt.Errorf("source branch %q drifted: expected head %s, live head %s", pr.SourceBranch, expected.Source, liveSource)
	}
	baseExists, err := repo.BranchExists(ctx, pr.BaseBranch)
	if err != nil {
		return err
	}
	if !baseExists {
		return fmt.Errorf("base branch %q is missing: expected head %s, live identity is deleted", pr.BaseBranch, expected.Base)
	}
	liveBase, err := repo.HeadSHA(ctx, "refs/heads/"+pr.BaseBranch)
	if err != nil {
		return err
	}
	if liveBase != expected.Base {
		return fmt.Errorf("base branch %q drifted: expected head %s, live head %s", pr.BaseBranch, expected.Base, liveBase)
	}
	return nil
}
