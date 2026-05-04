package model

import "time"

type Status string

const (
	StatusOpen     Status = "open"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

type Config struct {
	DefaultBranch string `yaml:"default_branch,omitempty"`
}

type PR struct {
	ID                 string          `yaml:"id"`
	Title              string          `yaml:"title"`
	SourceBranch       string          `yaml:"source_branch"`
	SourceWorktreePath string          `yaml:"source_worktree_path"`
	RepositoryRoot     string          `yaml:"repository_root"`
	BaseBranch         string          `yaml:"base_branch"`
	SourceHeadSHA      string          `yaml:"source_head_sha"`
	BaseHeadSHA        string          `yaml:"base_head_sha"`
	MergeBaseSHA       string          `yaml:"merge_base_sha,omitempty"`
	Description        string          `yaml:"description"`
	FileDiffs          []FileDiff      `yaml:"file_diffs"`
	Commits            []Commit        `yaml:"commits"`
	Comments           []Comment       `yaml:"comments,omitempty"`
	MergeConflicts     []MergeConflict `yaml:"merge_conflicts,omitempty"`
	Status             Status          `yaml:"status"`
	CreatedAt          time.Time       `yaml:"created_at"`
	UpdatedAt          time.Time       `yaml:"updated_at"`
	ClosedAt           *time.Time      `yaml:"closed_at,omitempty"`
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

type Commit struct {
	SHA     string `yaml:"sha"`
	Message string `yaml:"message"`
}

type Comment struct {
	FilePath  string    `yaml:"file_path"`
	LineStart int       `yaml:"line_start"`
	LineEnd   int       `yaml:"line_end"`
	Comment   string    `yaml:"comment"`
	CommitSHA string    `yaml:"commit_sha"`
	CreatedAt time.Time `yaml:"created_at"`
}

type MergeConflict struct {
	Path    string `yaml:"path,omitempty"`
	Message string `yaml:"message"`
}
