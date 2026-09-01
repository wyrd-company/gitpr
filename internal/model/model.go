package model

import "time"

type Config struct {
	DefaultBranch string `yaml:"default_branch,omitempty"`
}

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateMerged PRState = "merged"
	PRStateClosed PRState = "closed"
)

type ClosureReason string

const (
	ClosureIntegrated ClosureReason = "integrated"
	ClosureSuperseded ClosureReason = "superseded"
	ClosureAbandoned  ClosureReason = "abandoned"
)

type Closure struct {
	Reason                    ClosureReason `yaml:"reason"`
	DestinationBranch         string        `yaml:"destination_branch,omitempty"`
	ResultingCommitSHAs       []string      `yaml:"resulting_commit_shas,omitempty"`
	PatchEquivalentIdentities []string      `yaml:"patch_equivalent_identities,omitempty"`
	ReplacingPRID             string        `yaml:"replacing_pr_id,omitempty"`
	Note                      string        `yaml:"note,omitempty"`
}

type ReviewVerdict string

const (
	VerdictAccepted ReviewVerdict = "accepted"
	VerdictRejected ReviewVerdict = "rejected"
)

type ReviewEvent struct {
	ID                 string        `yaml:"id"`
	SourceHeadSHA      string        `yaml:"source_head_sha"`
	BaseHeadSHA        string        `yaml:"base_head_sha"`
	MergeBaseSHA       string        `yaml:"merge_base_sha"`
	Verdict            ReviewVerdict `yaml:"verdict"`
	Timestamp          time.Time     `yaml:"timestamp"`
	PredecessorEventID string        `yaml:"predecessor_event_id,omitempty"`
}

type ThreadKind string
type ThreadStatus string
type DiffSide string

const (
	ThreadAnchored ThreadKind   = "anchored"
	ThreadPRLevel  ThreadKind   = "pr-level"
	ThreadOpen     ThreadStatus = "open"
	ThreadResolved ThreadStatus = "resolved"
	DiffSideSource DiffSide     = "source"
	DiffSideBase   DiffSide     = "base"
)

type ThreadAnchor struct {
	SourceHeadSHA string   `yaml:"source_head_sha"`
	BaseHeadSHA   string   `yaml:"base_head_sha"`
	File          string   `yaml:"file"`
	Side          DiffSide `yaml:"side"`
	LineStart     int      `yaml:"line_start"`
	LineEnd       int      `yaml:"line_end"`
}

type ThreadComment struct {
	Body          string    `yaml:"body"`
	Timestamp     time.Time `yaml:"timestamp"`
	SourceHeadSHA string    `yaml:"source_head_sha"`
	BaseHeadSHA   string    `yaml:"base_head_sha"`
	PostClosure   bool      `yaml:"post_closure"`
}

type Thread struct {
	ID       string          `yaml:"id"`
	Kind     ThreadKind      `yaml:"kind"`
	Status   ThreadStatus    `yaml:"status"`
	Outdated bool            `yaml:"outdated"`
	Anchor   *ThreadAnchor   `yaml:"anchor,omitempty"`
	Comments []ThreadComment `yaml:"comments,omitempty"`
}

type PR2 struct {
	Schema             int           `yaml:"schema"`
	ID                 string        `yaml:"id"`
	Title              string        `yaml:"title"`
	SourceBranch       string        `yaml:"source_branch"`
	SourceWorktreePath string        `yaml:"source_worktree_path"`
	BaseBranch         string        `yaml:"base_branch"`
	RepositoryRoot     string        `yaml:"repository_root"`
	Description        string        `yaml:"description"`
	State              PRState       `yaml:"state"`
	Closure            *Closure      `yaml:"closure,omitempty"`
	Events             []ReviewEvent `yaml:"events,omitempty"`
	Threads            []Thread      `yaml:"threads,omitempty"`
	CreatedAt          time.Time     `yaml:"created_at"`
	UpdatedAt          time.Time     `yaml:"updated_at"`
	MergedAt           *time.Time    `yaml:"merged_at,omitempty"`
	MergedEventID      string        `yaml:"merged_event_id,omitempty"`
	ClosedAt           *time.Time    `yaml:"closed_at,omitempty"`
}

type FileDiff struct {
	OldPath string `yaml:"old_path,omitempty"`
	NewPath string `yaml:"new_path,omitempty"`
	Status  string `yaml:"status"`
	Patch   string `yaml:"patch"`
	Hunks   []Hunk `yaml:"hunks"`
}

type Hunk struct {
	Header   string     `yaml:"header"`
	OldStart int        `yaml:"old_start"`
	OldLines int        `yaml:"old_lines"`
	NewStart int        `yaml:"new_start"`
	NewLines int        `yaml:"new_lines"`
	Lines    []DiffLine `yaml:"lines"`
}

type DiffLine struct {
	Kind    string `yaml:"kind"`
	OldLine int    `yaml:"old_line,omitempty"`
	NewLine int    `yaml:"new_line,omitempty"`
	Content string `yaml:"content"`
}
