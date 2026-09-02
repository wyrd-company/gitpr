package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

// EditPRRequest carries metadata corrections for an open branch-based PR.
// A nil field is left unchanged; a non-nil field replaces the stored value
// verbatim (Description is never trimmed so byte-preserving corrections,
// such as ones read from a file, survive intact).
type EditPRRequest struct {
	Title       *string
	Description *string
}

// EditPR replaces title and/or description metadata on an open PR without
// creating a new record, changing its source/base heads, or touching any
// other metadata. It refuses closed or merged PRs before writing anything.
func (s *Service) EditPR(id string, req EditPRRequest) (model.PR2, string, error) {
	if req.Title == nil && req.Description == nil {
		return model.PR2{}, "", errors.New("edit requires --title, --description, or --description-file")
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		return model.PR2{}, "", errors.New("title cannot be empty")
	}

	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR(id)
		if err != nil {
			return model.PR2{}, "", err
		}
		if pr.State != model.PRStateOpen {
			return model.PR2{}, "", fmt.Errorf("cannot edit PR %s in %s state; only open PRs can be edited", pr.ID, pr.State)
		}
		if req.Title != nil {
			pr.Title = strings.TrimSpace(*req.Title)
		}
		if req.Description != nil {
			pr.Description = *req.Description
		}
		pr.UpdatedAt = time.Now().UTC()
		ref, err := s.store.SavePR2(pr, model.PRStateOpen, version)
		if err == nil {
			return pr, ref, nil
		}
		if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR2{}, "", err
		}
	}
	return model.PR2{}, "", fmt.Errorf("%w after %d attempts; retry edit", store.ErrMetadataConflict, metadataMutationAttempts)
}
