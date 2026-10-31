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

// InterdiffSummary counts changes to diff entries, comparing blob IDs and modes.
// Each compared diff represents renames as deletion and addition entries.
// No patches are computed.
type InterdiffSummary struct {
	PreviousFiles int `yaml:"previous_files"`
	CurrentFiles  int `yaml:"current_files"`
	Added         int `yaml:"added_to_diff"`
	Removed       int `yaml:"removed_from_diff"`
	Changed       int `yaml:"changed"`
	Unchanged     int `yaml:"unchanged"`
}

type ReviewReport struct {
	ID               string             `yaml:"id"`
	SourceBranch     string             `yaml:"source_branch"`
	BaseBranch       string             `yaml:"base_branch"`
	Basis            ReviewBasis        `yaml:"basis"`
	Diff             []model.FileDiff   `yaml:"diff,omitempty"`
	LatestEvent      *model.ReviewEvent `yaml:"latest_event,omitempty"`
	InterdiffStyle   string             `yaml:"interdiff_style,omitempty"`
	Interdiff        []InterdiffFile    `yaml:"interdiff,omitempty"`
	InterdiffSummary *InterdiffSummary  `yaml:"interdiff_summary,omitempty"`
	VerdictHint      string             `yaml:"verdict_hint,omitempty"`
	Threads          []model.Thread     `yaml:"threads,omitempty"`
	BaseTag          *model.TagBase     `yaml:"base_tag,omitempty"`
}

func (s *Service) ReviewPR(ctx context.Context, id string) (ReviewReport, error) {
	return s.reviewPR(ctx, id, false)
}

// ReviewPRSummary reports live review state without computing patch bodies.
func (s *Service) ReviewPRSummary(ctx context.Context, id string) (ReviewReport, error) {
	return s.reviewPR(ctx, id, true)
}

func (s *Service) reviewPR(ctx context.Context, id string, summary bool) (ReviewReport, error) {
	pr, _, err := s.store.LoadPR(id)
	if err != nil {
		return ReviewReport{}, err
	}
	repo, err := gitutil.Open(pr.RepositoryRoot)
	if err != nil {
		return ReviewReport{}, err
	}
	report := ReviewReport{ID: pr.ID, SourceBranch: pr.SourceBranch, BaseBranch: pr.BaseBranch, BaseTag: pr.BaseTag}
	if len(pr.Events) > 0 {
		latest := pr.Events[len(pr.Events)-1]
		report.LatestEvent = &latest
	}
	if pr.BaseTag != nil {
		if err := validateTagBase(ctx, repo, pr); err != nil {
			return ReviewReport{}, err
		}
		report.Basis.BaseHeadSHA = pr.BaseTag.CommitSHA
	} else {
		baseExists, err := repo.BranchExists(ctx, pr.BaseBranch)
		if err != nil {
			return ReviewReport{}, err
		}
		if !baseExists {
			report.Basis.BaseBranchMissing = true
			return report, nil
		}
		report.Basis.BaseHeadSHA, err = repo.HeadSHA(ctx, "refs/heads/"+pr.BaseBranch)
		if err != nil {
			return ReviewReport{}, err
		}
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
	if !summary {
		report.Diff, err = repo.FileDiffs(ctx, report.Basis.MergeBaseSHA, report.Basis.SourceHeadSHA)
		if err != nil {
			return ReviewReport{}, err
		}
	}

	report.Threads = remapThreads(ctx, repo, pr.Threads, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA})
	if summary {
		for i := range report.Threads {
			report.Threads[i].Comments = nil
		}
		if report.LatestEvent == nil {
			return report, nil
		}
		return s.summarizeReviewInterdiff(ctx, repo, report)
	}
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

func (s *Service) summarizeReviewInterdiff(ctx context.Context, repo *gitutil.Repo, report ReviewReport) (ReviewReport, error) {
	previous, err := repo.DiffEntries(ctx, report.LatestEvent.MergeBaseSHA, report.LatestEvent.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	current, err := repo.DiffEntries(ctx, report.Basis.MergeBaseSHA, report.Basis.SourceHeadSHA)
	if err != nil {
		return ReviewReport{}, err
	}
	counts := &InterdiffSummary{PreviousFiles: len(previous), CurrentFiles: len(current)}
	for path, entry := range current {
		before, exists := previous[path]
		switch {
		case !exists:
			counts.Added++
		case before == entry:
			counts.Unchanged++
		default:
			counts.Changed++
		}
	}
	for path := range previous {
		if _, exists := current[path]; !exists {
			counts.Removed++
		}
	}
	report.InterdiffStyle = "file counts by blob IDs and modes (renames counted as removal and addition)"
	report.InterdiffSummary = counts
	return report, nil
}
