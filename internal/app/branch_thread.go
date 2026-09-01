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

type ThreadCommentRequest struct {
	ThreadID  string
	PRLevel   bool
	File      string
	Side      model.DiffSide
	LineStart int
	LineEnd   int
	Text      string
	Heads     *ExpectedHeads
}

func (s *Service) CommentPR2(ctx context.Context, id string, req ThreadCommentRequest) (model.PR2, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return model.PR2{}, "", errors.New("--text is required")
	}
	if req.ThreadID == "" {
		if req.PRLevel {
			if req.File != "" {
				return model.PR2{}, "", errors.New("--file cannot be used with --pr-level")
			}
			if req.LineStart != 0 {
				return model.PR2{}, "", errors.New("--line-start cannot be used with --pr-level")
			}
			if req.LineEnd != 0 {
				return model.PR2{}, "", errors.New("--line-end cannot be used with --pr-level")
			}
			if req.Side != "" && req.Side != model.DiffSideSource {
				return model.PR2{}, "", errors.New("--side cannot be used with --pr-level")
			}
		} else {
			if strings.TrimSpace(req.File) == "" {
				return model.PR2{}, "", errors.New("--file is required unless --pr-level is used")
			}
			if req.LineStart <= 0 {
				return model.PR2{}, "", errors.New("--line-start must be greater than 0")
			}
			if req.LineEnd == 0 {
				req.LineEnd = req.LineStart
			}
			if req.LineEnd < req.LineStart {
				return model.PR2{}, "", errors.New("--line-end must be greater than or equal to --line-start")
			}
			if req.Side == "" {
				req.Side = model.DiffSideSource
			}
			if req.Side != model.DiffSideSource && req.Side != model.DiffSideBase {
				return model.PR2{}, "", errors.New("thread side must be source or base")
			}
		}
	}
	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR2(id)
		if err != nil {
			return model.PR2{}, "", err
		}
		repo, err := gitutil.Open(pr.RepositoryRoot)
		if err != nil {
			return model.PR2{}, "", err
		}
		heads, err := resolveCommentHeads(ctx, repo, pr, req.Heads)
		if err != nil {
			return model.PR2{}, "", err
		}
		now := time.Now().UTC()
		comment := model.ThreadComment{Body: strings.TrimSpace(req.Text), Timestamp: now, SourceHeadSHA: heads.Source, BaseHeadSHA: heads.Base, PostClosure: pr.State != model.PRStateOpen}
		if req.ThreadID == "" {
			thread := model.Thread{ID: ulid.Make().String(), Status: model.ThreadOpen, Comments: []model.ThreadComment{comment}}
			if req.PRLevel {
				thread.Kind = model.ThreadPRLevel
			} else {
				thread.Kind = model.ThreadAnchored
				thread.Anchor = &model.ThreadAnchor{SourceHeadSHA: heads.Source, BaseHeadSHA: heads.Base, File: strings.TrimSpace(req.File), Side: req.Side, LineStart: req.LineStart, LineEnd: req.LineEnd}
			}
			pr.Threads = append(pr.Threads, thread)
		} else {
			idx := threadIndex(pr.Threads, req.ThreadID)
			if idx < 0 {
				return model.PR2{}, "", fmt.Errorf("thread %s not found on PR %s", req.ThreadID, pr.ID)
			}
			pr.Threads[idx] = remapThread(ctx, repo, pr.Threads[idx], *heads)
			pr.Threads[idx].Comments = append(pr.Threads[idx].Comments, comment)
		}
		pr.UpdatedAt = now
		ref, err := s.store.SavePR2(pr, pr.State, version)
		if err == nil {
			return pr, ref, nil
		}
		if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR2{}, "", err
		}
	}
	return model.PR2{}, "", fmt.Errorf("%w after %d attempts; retry comment", store.ErrMetadataConflict, metadataMutationAttempts)
}

func resolveCommentHeads(ctx context.Context, repo *gitutil.Repo, pr model.PR2, explicit *ExpectedHeads) (*ExpectedHeads, error) {
	if explicit != nil {
		if err := explicit.Validate(); err != nil {
			return nil, err
		}
		if !repo.CommitExists(ctx, explicit.Source) {
			return nil, fmt.Errorf("comment source head %s does not resolve to a commit", explicit.Source)
		}
		if !repo.CommitExists(ctx, explicit.Base) {
			return nil, fmt.Errorf("comment base head %s does not resolve to a commit", explicit.Base)
		}
		copy := *explicit
		return &copy, nil
	}
	if exists, err := repo.BranchExists(ctx, pr.SourceBranch); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("source branch %q is missing; pass the explicit pair from gitpr review with --basis or --source-head and --base-head", pr.SourceBranch)
	}
	if exists, err := repo.BranchExists(ctx, pr.BaseBranch); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("base branch %q is missing; pass an explicit review basis", pr.BaseBranch)
	}
	source, err := repo.HeadSHA(ctx, "refs/heads/"+pr.SourceBranch)
	if err != nil {
		return nil, err
	}
	base, err := repo.HeadSHA(ctx, "refs/heads/"+pr.BaseBranch)
	if err != nil {
		return nil, err
	}
	return &ExpectedHeads{Source: source, Base: base}, nil
}

func (s *Service) SetThreadStatus(id, threadID string, status model.ThreadStatus) (model.PR2, string, error) {
	for attempt := 0; attempt < metadataMutationAttempts; attempt++ {
		pr, version, err := s.store.LoadPR2(id)
		if err != nil {
			return model.PR2{}, "", err
		}
		idx := threadIndex(pr.Threads, threadID)
		if idx < 0 {
			return model.PR2{}, "", fmt.Errorf("thread %s not found on PR %s", threadID, pr.ID)
		}
		pr.Threads[idx].Status = status
		pr.UpdatedAt = time.Now().UTC()
		ref, err := s.store.SavePR2(pr, pr.State, version)
		if err == nil {
			return pr, ref, nil
		}
		if !errors.Is(err, store.ErrMetadataConflict) {
			return model.PR2{}, "", err
		}
	}
	return model.PR2{}, "", fmt.Errorf("%w after %d attempts; retry thread update", store.ErrMetadataConflict, metadataMutationAttempts)
}

func threadIndex(threads []model.Thread, id string) int {
	for i := range threads {
		if threads[i].ID == id {
			return i
		}
	}
	return -1
}

func remapThreads(ctx context.Context, repo *gitutil.Repo, threads []model.Thread, heads ExpectedHeads) []model.Thread {
	result := append([]model.Thread(nil), threads...)
	for i := range result {
		result[i] = remapThread(ctx, repo, result[i], heads)
	}
	return result
}

func remapThread(ctx context.Context, repo *gitutil.Repo, thread model.Thread, heads ExpectedHeads) model.Thread {
	if thread.Kind == model.ThreadPRLevel || thread.Anchor == nil {
		thread.Outdated = false
		return thread
	}
	anchor := *thread.Anchor
	oldRef, newRef := anchor.SourceHeadSHA, heads.Source
	if anchor.Side == model.DiffSideBase {
		oldRef, newRef = anchor.BaseHeadSHA, heads.Base
	}
	oldText, oldErr := repo.FileContentAtRef(ctx, oldRef, anchor.File)
	newText, newErr := repo.FileContentAtRef(ctx, newRef, anchor.File)
	if oldErr != nil || newErr != nil {
		thread.Outdated = true
		return thread
	}
	start, end, ok := mapUnchangedRange(splitFileLines(oldText), splitFileLines(newText), anchor.LineStart, anchor.LineEnd)
	if !ok {
		thread.Outdated = true
		return thread
	}
	anchor.SourceHeadSHA, anchor.BaseHeadSHA = heads.Source, heads.Base
	thread.Anchor = &anchor
	thread.Anchor.LineStart, thread.Anchor.LineEnd, thread.Outdated = start, end, false
	return thread
}

func splitFileLines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

const remapLineLimit = 50_000

type lineMatch struct{ old, new int }

// mapUnchangedRange trims unchanged edges, then walks an LCS diff with
// Hirschberg's two-row algorithm so memory is linear in the shorter input.
func mapUnchangedRange(oldLines, newLines []string, start, end int) (int, int, bool) {
	if start <= 0 || end < start || end > len(oldLines) {
		return 0, 0, false
	}
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	oldEnd, newEnd := len(oldLines), len(newLines)
	for oldEnd > prefix && newEnd > prefix && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	if oldEnd-prefix > remapLineLimit || newEnd-prefix > remapLineLimit {
		return 0, 0, false
	}
	mapping := make(map[int]int, prefix+(len(oldLines)-oldEnd))
	for i := 0; i < prefix; i++ {
		mapping[i+1] = i + 1
	}
	for _, match := range lcsMatches(oldLines[prefix:oldEnd], newLines[prefix:newEnd]) {
		mapping[prefix+match.old+1] = prefix + match.new + 1
	}
	shift := newEnd - oldEnd
	for i := oldEnd; i < len(oldLines); i++ {
		mapping[i+1] = i + shift + 1
	}
	newStart, ok := mapping[start]
	if !ok {
		return 0, 0, false
	}
	for line := start; line <= end; line++ {
		mapped, ok := mapping[line]
		if !ok || mapped != newStart+(line-start) {
			return 0, 0, false
		}
	}
	return newStart, newStart + (end - start), true
}

func lcsMatches(a, b []string) []lineMatch {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	if len(b) > len(a) {
		swapped := lcsMatches(b, a)
		for i := range swapped {
			swapped[i].old, swapped[i].new = swapped[i].new, swapped[i].old
		}
		return swapped
	}
	if len(a) == 1 {
		for j := range b {
			if a[0] == b[j] {
				return []lineMatch{{old: 0, new: j}}
			}
		}
		return nil
	}
	mid := len(a) / 2
	left := lcsScore(a[:mid], b)
	right := lcsScoreReverse(a[mid:], b)
	split := 0
	for j := 1; j <= len(b); j++ {
		if left[j]+right[len(b)-j] > left[split]+right[len(b)-split] {
			split = j
		}
	}
	first := lcsMatches(a[:mid], b[:split])
	second := lcsMatches(a[mid:], b[split:])
	for i := range second {
		second[i].old += mid
		second[i].new += split
	}
	return append(first, second...)
}

func lcsScore(a, b []string) []int {
	row := make([]int, len(b)+1)
	for _, av := range a {
		previous := 0
		for j, bv := range b {
			above := row[j+1]
			if av == bv {
				row[j+1] = previous + 1
			} else if row[j] > row[j+1] {
				row[j+1] = row[j]
			}
			previous = above
		}
	}
	return row
}

func lcsScoreReverse(a, b []string) []int {
	reversedA := append([]string(nil), a...)
	reversedB := append([]string(nil), b...)
	for i, j := 0, len(reversedA)-1; i < j; i, j = i+1, j-1 {
		reversedA[i], reversedA[j] = reversedA[j], reversedA[i]
	}
	for i, j := 0, len(reversedB)-1; i < j; i, j = i+1, j-1 {
		reversedB[i], reversedB[j] = reversedB[j], reversedB[i]
	}
	return lcsScore(reversedA, reversedB)
}
