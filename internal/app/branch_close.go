package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

type ClosePRRequest struct {
	Reason       model.ClosureReason
	Destination  string
	Commits      []string
	PatchIDs     []string
	SupersededBy string
	Note         string
}

type DeleteSummary struct {
	ID          string
	State       string
	EventCount  int
	ThreadCount int
}

func (s *Service) ClosePR(id string, req ClosePRRequest) (model.PR2, string, error) {
	record, _, err := s.store.LoadPR(id)
	if err != nil {
		return model.PR2{}, "", err
	}
	if _, legacy := record.(model.PR); legacy {
		return model.PR2{}, "", errors.New("close is available only for branch-based PRs; legacy records retain their historical status workflow")
	}
	closure, err := s.validateClosure(id, req)
	if err != nil {
		return model.PR2{}, "", err
	}
	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR2(id)
		if err != nil {
			return model.PR2{}, "", err
		}
		if pr.State != model.PRStateOpen {
			return model.PR2{}, "", fmt.Errorf("cannot close PR %s in %s state; only open PRs can close", pr.ID, pr.State)
		}
		now := time.Now().UTC()
		pr.State = model.PRStateClosed
		pr.Closure = closure
		pr.ClosedAt = &now
		pr.UpdatedAt = now
		ref, err := s.store.SavePR2(pr, model.PRStateOpen, version)
		if err == nil {
			return pr, ref, nil
		}
		if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR2{}, "", err
		}
	}
	return model.PR2{}, "", fmt.Errorf("%w after %d attempts; retry close", store.ErrMetadataConflict, metadataMutationAttempts)
}

func (s *Service) validateClosure(id string, req ClosePRRequest) (*model.Closure, error) {
	closure := &model.Closure{Reason: req.Reason, Note: strings.TrimSpace(req.Note)}
	switch req.Reason {
	case model.ClosureIntegrated:
		if req.SupersededBy != "" {
			return nil, errors.New("--superseded-by does not apply to --reason integrated")
		}
		closure.DestinationBranch = strings.TrimSpace(req.Destination)
		if closure.DestinationBranch == "" {
			return nil, errors.New("integrated closure requires --destination")
		}
		if len(req.Commits) == 0 && len(req.PatchIDs) == 0 {
			return nil, errors.New("integrated closure requires at least one --commit or --patch-id")
		}
		for _, commit := range req.Commits {
			if !isFullObjectID(commit) {
				return nil, errors.New("each --commit must be a full 40-character lowercase hexadecimal object ID")
			}
		}
		closure.ResultingCommitSHAs = append([]string(nil), req.Commits...)
		for _, id := range req.PatchIDs {
			if strings.TrimSpace(id) == "" {
				return nil, errors.New("--patch-id cannot be empty")
			}
		}
		closure.PatchEquivalentIdentities = append([]string(nil), req.PatchIDs...)
	case model.ClosureSuperseded:
		if req.Destination != "" {
			return nil, errors.New("--destination does not apply to --reason superseded")
		}
		if len(req.Commits) > 0 {
			return nil, errors.New("--commit does not apply to --reason superseded")
		}
		if len(req.PatchIDs) > 0 {
			return nil, errors.New("--patch-id does not apply to --reason superseded")
		}
		closure.ReplacingPRID = strings.TrimSpace(req.SupersededBy)
		if closure.ReplacingPRID == "" {
			return nil, errors.New("superseded closure requires --superseded-by")
		}
		if closure.ReplacingPRID == id {
			return nil, errors.New("--superseded-by cannot name the PR being closed for --reason superseded")
		}
		if _, _, err := s.store.LoadPR2(closure.ReplacingPRID); err != nil {
			return nil, fmt.Errorf("--superseded-by must name an existing branch-based PR: %w", err)
		}
	case model.ClosureAbandoned:
		if req.Destination != "" {
			return nil, errors.New("--destination does not apply to --reason abandoned")
		}
		if len(req.Commits) > 0 {
			return nil, errors.New("--commit does not apply to --reason abandoned")
		}
		if len(req.PatchIDs) > 0 {
			return nil, errors.New("--patch-id does not apply to --reason abandoned")
		}
		if req.SupersededBy != "" {
			return nil, errors.New("--superseded-by does not apply to --reason abandoned")
		}
	default:
		return nil, errors.New("--reason must be integrated, superseded, or abandoned")
	}
	return closure, nil
}

func (s *Service) DeleteRecordSummary(id string) (DeleteSummary, error) {
	record, _, err := s.store.LoadPR(id)
	if err != nil {
		return DeleteSummary{}, err
	}
	summary := DeleteSummary{ID: record.RecordID(), State: record.RecordDisplayState()}
	if pr, ok := record.(model.PR2); ok {
		summary.EventCount = len(pr.Events)
		summary.ThreadCount = len(pr.Threads)
	} else if pr, ok := record.(model.PR); ok {
		summary.ThreadCount = len(pr.Comments)
	}
	return summary, nil
}

func (s *Service) ListPRsWithReason(state string, reason model.ClosureReason) ([]model.Record, error) {
	if reason == "" {
		return s.store.ListPRs(state)
	}
	if state != string(model.PRStateClosed) {
		return nil, errors.New("--reason can be combined only with --state closed")
	}
	records, err := s.store.ListPRs(state)
	if err != nil {
		return nil, err
	}
	result := make([]model.Record, 0)
	for _, record := range records {
		if pr, ok := record.(model.PR2); ok && pr.Closure != nil && pr.Closure.Reason == reason {
			result = append(result, pr)
		}
	}
	return result, nil
}

func (s *Service) DeleteRecord(id string) error {
	record, version, err := s.store.LoadPR(id)
	if err != nil {
		return err
	}
	switch pr := record.(type) {
	case model.PR2:
		return s.store.DeletePR2(pr, version)
	case model.PR:
		return s.store.DeletePR(pr, version)
	default:
		return fmt.Errorf("unsupported record type %T", record)
	}
}
