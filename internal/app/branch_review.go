package app

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
)

type ReviewBasis struct {
	SourceHeadSHA       string `yaml:"source_head_sha,omitempty"`
	BaseHeadSHA         string `yaml:"base_head_sha"`
	SourceBranchMissing bool   `yaml:"source_branch_missing"`
	BaseBranchMissing   bool   `yaml:"base_branch_missing"`
	MergeBaseSHA        string `yaml:"merge_base_sha,omitempty"`
	BaseContained       *bool  `yaml:"base_contained,omitempty"`
}

type InterdiffFile struct {
	Path          string `yaml:"path"`
	Change        string `yaml:"change"`
	PreviousHunks int    `yaml:"previous_hunks"`
	CurrentHunks  int    `yaml:"current_hunks"`
}

type ReviewReport struct {
	ID             string             `yaml:"id"`
	SourceBranch   string             `yaml:"source_branch"`
	BaseBranch     string             `yaml:"base_branch"`
	Basis          ReviewBasis        `yaml:"basis"`
	Diff           []model.FileDiff   `yaml:"diff,omitempty"`
	LatestEvent    *model.ReviewEvent `yaml:"latest_event,omitempty"`
	InterdiffStyle string             `yaml:"interdiff_style,omitempty"`
	Interdiff      []InterdiffFile    `yaml:"interdiff,omitempty"`
	VerdictHint    string             `yaml:"verdict_hint,omitempty"`
	Threads        []model.Thread     `yaml:"threads,omitempty"`
}

func (s *Service) ReviewPR(ctx context.Context, id string) (ReviewReport, error) {
	pr, _, err := s.store.LoadPR2(id)
	if err != nil {
		return ReviewReport{}, err
	}
	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return ReviewReport{}, err
	}
	report := ReviewReport{ID: pr.ID, SourceBranch: pr.SourceBranch, BaseBranch: pr.BaseBranch}
	if len(pr.Events) > 0 {
		latest := pr.Events[len(pr.Events)-1]
		report.LatestEvent = &latest
	}
	baseRef := "refs/heads/" + pr.BaseBranch
	baseExists, err := repo.BranchExists(ctx, pr.BaseBranch)
	if err != nil {
		return ReviewReport{}, err
	}
	if !baseExists {
		report.Basis.BaseBranchMissing = true
		return report, nil
	}
	report.Basis.BaseHeadSHA, err = repo.HeadSHA(ctx, baseRef)
	if err != nil {
		return ReviewReport{}, fmt.Errorf("resolve base branch %q: %w", pr.BaseBranch, err)
	}
	sourceRef := "refs/heads/" + pr.SourceBranch
	exists, err := repo.BranchExists(ctx, pr.SourceBranch)
	if err != nil {
		return ReviewReport{}, err
	}
	if !exists {
		report.Basis.SourceBranchMissing = true
		return report, nil
	}
	report.Basis.SourceHeadSHA, err = repo.HeadSHA(ctx, sourceRef)
	if err != nil {
		return ReviewReport{}, fmt.Errorf("resolve source branch %q: %w", pr.SourceBranch, err)
	}
	report.VerdictHint = fmt.Sprintf("gitpr approve %s --source-head %s --base-head %s", pr.ID, report.Basis.SourceHeadSHA, report.Basis.BaseHeadSHA)
	report.Basis.MergeBaseSHA, err = repo.MergeBase(ctx, report.Basis.BaseHeadSHA, report.Basis.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	contained, err := repo.IsAncestor(ctx, report.Basis.BaseHeadSHA, report.Basis.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	report.Basis.BaseContained = &contained
	report.Diff, err = repo.FileDiffs(ctx, report.Basis.MergeBaseSHA, report.Basis.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	report.Threads = remapThreads(ctx, repo, pr.Threads, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA})
	if report.LatestEvent == nil {
		return report, nil
	}
	previous, err := repo.FileDiffs(ctx, report.LatestEvent.MergeBaseSHA, report.LatestEvent.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	report.InterdiffStyle = "file-set changes with previous/current hunk counts"
	report.Interdiff = summarizeInterdiff(previous, report.Diff)
	return report, nil
}

func summarizeInterdiff(previous, current []model.FileDiff) []InterdiffFile {
	old := diffsByPath(previous)
	now := diffsByPath(current)
	paths := make(map[string]struct{}, len(old)+len(now))
	for path := range old {
		paths[path] = struct{}{}
	}
	for path := range now {
		paths[path] = struct{}{}
	}
	result := make([]InterdiffFile, 0, len(paths))
	for path := range paths {
		before, hadBefore := old[path]
		after, hasAfter := now[path]
		change := "changed"
		switch {
		case !hadBefore:
			change = "added-to-diff"
		case !hasAfter:
			change = "removed-from-diff"
		case reflect.DeepEqual(before, after):
			continue
		}
		result = append(result, InterdiffFile{Path: path, Change: change, PreviousHunks: len(before.Hunks), CurrentHunks: len(after.Hunks)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func diffsByPath(diffs []model.FileDiff) map[string]model.FileDiff {
	result := make(map[string]model.FileDiff, len(diffs))
	for _, diff := range diffs {
		path := diff.NewPath
		if path == "" {
			path = diff.OldPath
		}
		result[path] = diff
	}
	return result
}
