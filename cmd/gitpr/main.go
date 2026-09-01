package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/tui"
)

var version = "dev"

func main() {
	rootCmd := newRootCmd()
	rootCmd.SetOut(os.Stdout)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "gitpr",
		Short:   "Review local git worktree branches as lightweight PRs",
		Version: version,
	}

	rootCmd.AddCommand(newCreateCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newReviewCmd())
	rootCmd.AddCommand(newApproveCmd())
	rootCmd.AddCommand(newCommentsCmd())
	rootCmd.AddCommand(newCommentCmd())
	rootCmd.AddCommand(newRefreshCmd())
	rootCmd.AddCommand(newRejectCmd())
	rootCmd.AddCommand(newMergeCmd())
	rootCmd.AddCommand(newCloseCmd())
	rootCmd.AddCommand(newDeleteCmd())
	rootCmd.AddCommand(newDebugCmd())
	rootCmd.AddCommand(newTUICmd())

	return rootCmd
}

func newCreateCmd() *cobra.Command {
	var title string
	var description string
	var worktree string
	var base string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a PR snapshot from a local worktree branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceRoot := "."
			if strings.TrimSpace(worktree) != "" {
				serviceRoot = worktree
			}

			svc, err := app.NewService(serviceRoot)
			if err != nil {
				return err
			}

			req := app.CreatePRRequest{
				Title:       title,
				Description: description,
				Worktree:    worktree,
				BaseBranch:  base,
			}

			pr, ref, err := svc.CreatePR(cmd.Context(), req)
			if err != nil {
				return err
			}

			cmd.Printf("Created PR %s at %s\n", pr.ID, ref)
			return nil
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "PR title")
	cmd.Flags().StringVarP(&description, "description", "d", "", "PR description")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Source worktree path (defaults to current directory)")
	cmd.Flags().StringVar(&base, "base", "", "Override detected default branch")
	_ = cmd.MarkFlagRequired("title")

	return cmd
}

func newListCmd() *cobra.Command {
	var state, status, reason string
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("state") && cmd.Flags().Changed("status") {
				return errors.New("use --state or --status, not both")
			}
			filter := state
			if cmd.Flags().Changed("status") {
				filter = status
			}
			if all {
				if cmd.Flags().Changed("state") || cmd.Flags().Changed("status") || reason != "" {
					return errors.New("--all cannot be combined with --state, --status, or --reason")
				}
				filter = "all"
			}
			prs, err := svc.ListPRsWithReason(filter, model.ClosureReason(reason))
			if err != nil {
				return err
			}

			if len(prs) == 0 {
				cmd.Println("No PRs found.")
				return nil
			}

			cmd.Printf("%-14s %-10s %-20s %s\n", "ID", "STATUS", "BRANCH", "TITLE")
			for _, pr := range prs {
				branch, title, suffix := recordListFields(pr)
				cmd.Printf("%-14s %-10s %-20s %s%s\n", shortID(pr.RecordID()), pr.RecordDisplayState(), branch, title, suffix)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&state, "state", "open", "Filter by state: open|approved|rejected|merged|closed")
	cmd.Flags().StringVar(&status, "status", "", "Deprecated alias for --state")
	cmd.Flags().StringVar(&reason, "reason", "", "Filter closed branch-based PRs by reason")
	cmd.Flags().BoolVar(&all, "all", false, "Show records in every vocabulary and state")
	return cmd
}

func newShowCmd() *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "show [pr-id]",
		Short: "Show a PR file as YAML",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			var targetID string
			if len(args) == 1 {
				targetID = args[0]
			} else {
				prs, err := svc.ListPRs(status)
				if err != nil {
					return err
				}
				if len(prs) == 0 {
					return fmt.Errorf("no PRs found for status %q", status)
				}

				title := fmt.Sprintf("Select PR to show (%s)", status)
				targetID, err = tui.SelectPR(title, prs)
				if err != nil {
					return err
				}
				if targetID == "" {
					return nil
				}
			}

			pr, _, err := svc.LoadRecord(targetID)
			if err != nil {
				return err
			}

			out, err := yaml.Marshal(pr)
			if err != nil {
				return err
			}

			cmd.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by status when no PR ID is provided: open|approved|rejected|merged|closed|all")
	return cmd
}

func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review <pr-id>",
		Short: "Compute the live review basis for a branch-based PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}
			report, err := svc.ReviewPR(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out, err := yaml.Marshal(report)
			if err != nil {
				return err
			}
			cmd.Print(string(out))
			return nil
		},
	}
	return cmd
}

type expectedHeadFlags struct {
	source string
	base   string
	basis  string
}

func (f *expectedHeadFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.source, "source-head", "", "Expected source head from gitpr review")
	cmd.Flags().StringVar(&f.base, "base-head", "", "Expected base head from gitpr review")
	cmd.Flags().StringVar(&f.basis, "basis", "", "Expected review basis as <source>:<base>")
}

func (f expectedHeadFlags) parse() (*app.ExpectedHeads, error) {
	source, base, basis := strings.TrimSpace(f.source), strings.TrimSpace(f.base), strings.TrimSpace(f.basis)
	if basis == "" && source == "" && base == "" {
		return nil, nil
	}
	if basis == "" && (source == "" || base == "") {
		return nil, errors.New("both --source-head and --base-head are required; run gitpr review <id> before recording a verdict")
	}
	if basis != "" {
		if source != "" || base != "" {
			return nil, errors.New("use either --basis or --source-head with --base-head, not both")
		}
		parts := strings.Split(basis, ":")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("--basis must be <source-head>:<base-head> from gitpr review")
		}
		source, base = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	heads := &app.ExpectedHeads{Source: source, Base: base}
	if err := heads.Validate(); err != nil {
		return nil, err
	}
	return heads, nil
}

func newApproveCmd() *cobra.Command {
	var flags expectedHeadFlags
	cmd := &cobra.Command{
		Use:   "approve <pr-id>",
		Short: "Record an accepted review event for a branch-based PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			heads, err := flags.parse()
			if err != nil {
				return err
			}
			if heads == nil {
				heads = &app.ExpectedHeads{}
			}
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}
			pr, ref, err := svc.ApprovePR(cmd.Context(), args[0], *heads)
			if err != nil {
				return err
			}
			cmd.Printf("Approved PR %s at %s\n", shortID(pr.ID), ref)
			return nil
		},
	}
	flags.bind(cmd)
	return cmd
}

func recordListFields(record model.Record) (branch, title, suffix string) {
	switch pr := record.(type) {
	case model.PR:
		return pr.SourceBranch, pr.Title, ""
	case model.PR2:
		if pr.State == model.PRStateClosed && pr.Closure != nil {
			suffix = " (" + string(pr.Closure.Reason) + ")"
		}
		return pr.SourceBranch, pr.Title, suffix
	default:
		return "", "", ""
	}
}

func newCommentsCmd() *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "comments [pr-id]",
		Short: "Show only comments from a PR",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			var targetID string
			if len(args) == 1 {
				targetID = args[0]
			} else {
				prs, err := svc.ListPRs(status)
				if err != nil {
					return err
				}
				if len(prs) == 0 {
					return fmt.Errorf("no PRs found for status %q", status)
				}

				title := fmt.Sprintf("Select PR comments to show (%s)", status)
				targetID, err = tui.SelectPR(title, prs)
				if err != nil {
					return err
				}
				if targetID == "" {
					return nil
				}
			}

			pr, _, err := svc.LoadCommentsPR(targetID)
			if err != nil {
				return err
			}

			payload := struct {
				ID       string          `yaml:"id"`
				Title    string          `yaml:"title"`
				Status   model.Status    `yaml:"status"`
				Comments []model.Comment `yaml:"comments"`
			}{
				ID:       pr.ID,
				Title:    pr.Title,
				Status:   pr.Status,
				Comments: pr.Comments,
			}

			out, err := yaml.Marshal(payload)
			if err != nil {
				return err
			}

			cmd.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by status when no PR ID is provided: open|approved|rejected|merged|closed|all")
	return cmd
}

func newCommentCmd() *cobra.Command {
	var filePath string
	var lineStart int
	var lineEnd int
	var text string
	var commitSHA string
	var updateIndex int

	cmd := &cobra.Command{
		Use:   "comment <pr-id>",
		Short: "Add a review comment to an open PR (appends at the same anchor)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(filePath) == "" {
				return errors.New("--file is required")
			}
			if lineStart <= 0 {
				return errors.New("--line-start must be greater than 0")
			}
			if lineEnd <= 0 {
				lineEnd = lineStart
			}
			if lineEnd < lineStart {
				return errors.New("--line-end must be greater than or equal to --line-start")
			}
			if strings.TrimSpace(text) == "" {
				return errors.New("--text is required")
			}
			if cmd.Flags().Changed("update") && updateIndex < 0 {
				return errors.New("--update must be greater than or equal to 0")
			}

			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			comment := model.Comment{
				FilePath:  strings.TrimSpace(filePath),
				LineStart: lineStart,
				LineEnd:   lineEnd,
				Comment:   text,
				CommitSHA: strings.TrimSpace(commitSHA),
			}

			var pr model.PR
			if updateIndex >= 0 {
				pr, err = svc.UpdateComment(args[0], updateIndex, comment)
				if err != nil {
					return err
				}
				cmd.Printf("Updated comment %d on PR %s: %s:%d-%d\n", updateIndex, shortID(pr.ID), comment.FilePath, comment.LineStart, comment.LineEnd)
				return nil
			}

			pr, err = svc.AddComment(args[0], comment)
			if err != nil {
				return err
			}

			cmd.Printf("Saved comment on PR %s: %s:%d-%d\n", shortID(pr.ID), comment.FilePath, comment.LineStart, comment.LineEnd)
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Changed file path for the comment")
	cmd.Flags().IntVar(&lineStart, "line-start", 0, "Starting line number")
	cmd.Flags().IntVar(&lineEnd, "line-end", 0, "Ending line number (defaults to line-start)")
	cmd.Flags().StringVar(&text, "text", "", "Comment text")
	cmd.Flags().StringVar(&commitSHA, "commit", "", "Optional commit SHA")
	cmd.Flags().IntVar(&updateIndex, "update", -1, "Replace the comment at this index instead of appending")

	return cmd
}

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh <pr-id>",
		Short: "Refresh merge-conflict metadata for an open PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			pr, err := svc.RefreshPR(ctx, args[0])
			if err != nil {
				return err
			}

			if len(pr.MergeConflicts) == 0 {
				cmd.Printf("PR %s has no merge conflicts\n", shortID(pr.ID))
				return nil
			}

			cmd.Printf("PR %s has %d merge conflict(s):\n", shortID(pr.ID), len(pr.MergeConflicts))
			for _, conflict := range pr.MergeConflicts {
				if strings.TrimSpace(conflict.Path) != "" {
					cmd.Printf("- %s: %s\n", conflict.Path, conflict.Message)
				} else {
					cmd.Printf("- %s\n", conflict.Message)
				}
			}
			return nil
		},
	}

	return cmd
}

func newRejectCmd() *cobra.Command {
	var flags expectedHeadFlags
	cmd := &cobra.Command{
		Use:     "reject <pr-id>",
		Aliases: []string{"request-changes"},
		Short:   "Close an open PR as rejected",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			heads, err := flags.parse()
			if err != nil {
				return err
			}
			record, ref, err := svc.RejectRecord(cmd.Context(), args[0], heads)
			if err != nil {
				return err
			}

			cmd.Printf("Rejected PR %s at %s\n", shortID(record.RecordID()), ref)
			return nil
		},
	}
	flags.bind(cmd)

	return cmd
}

func newMergeCmd() *cobra.Command {
	var cleanup bool

	cmd := &cobra.Command{
		Use:   "merge <pr-id>",
		Short: "Merge an open PR into its base branch and mark it approved",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			record, ref, err := svc.MergeRecord(ctx, args[0], cleanup)
			if err != nil {
				if record != nil && ref != "" {
					if printErr := printMergeRecordSuccess(cmd, record, ref, cleanup); printErr != nil {
						return printErr
					}
					if _, legacy := record.(model.PR); legacy {
						cmd.PrintErrf("Cleanup failed after merge: %v\n", err)
						return nil
					}
				}
				return err
			}

			return printMergeRecordSuccess(cmd, record, ref, cleanup)
		},
	}

	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "Remove the source worktree and branch after a successful merge")
	return cmd
}

func printMergeRecordSuccess(cmd *cobra.Command, record model.Record, ref string, cleanup bool) error {
	if pr, legacy := record.(model.PR); legacy {
		printMergeSuccess(cmd, pr, ref, cleanup)
		return nil
	}
	pr, ok := record.(model.PR2)
	if !ok {
		return fmt.Errorf("cannot render merge result for record type %T", record)
	}
	cmd.Printf("Merged PR %s into %s at %s\n", shortID(pr.ID), pr.BaseBranch, ref)
	if !cleanup {
		cmd.Println("Source worktree kept.")
	}
	return nil
}

func printMergeSuccess(cmd *cobra.Command, pr model.PR, ref string, cleanup bool) {
	cmd.Printf("Merged PR %s into %s at %s\n", shortID(pr.ID), pr.BaseBranch, ref)
	if !cleanup {
		cmd.Println("Source worktree kept.")
	}
}

func newCloseCmd() *cobra.Command {
	var reason, destination, supersededBy, note string
	var commits, patchIDs []string
	cmd := &cobra.Command{Use: "close <pr-id>", Short: "Close an open branch-based PR", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := app.NewService(".")
		if err != nil {
			return err
		}
		pr, ref, err := svc.ClosePR(args[0], app.ClosePRRequest{Reason: model.ClosureReason(reason), Destination: destination, Commits: commits, PatchIDs: patchIDs, SupersededBy: supersededBy, Note: note})
		if err != nil {
			return err
		}
		cmd.Printf("Closed PR %s as %s at %s\n", shortID(pr.ID), pr.Closure.Reason, ref)
		return nil
	}}
	cmd.Flags().StringVar(&reason, "reason", "", "Closure reason: integrated|superseded|abandoned")
	cmd.Flags().StringVar(&destination, "destination", "", "Destination branch for integrated closure")
	cmd.Flags().StringSliceVar(&commits, "commit", nil, "Resulting commit SHA for integrated closure (repeatable)")
	cmd.Flags().StringSliceVar(&patchIDs, "patch-id", nil, "Patch-equivalent identity for integrated closure (repeatable)")
	cmd.Flags().StringVar(&supersededBy, "superseded-by", "", "Replacement branch-based PR ID")
	cmd.Flags().StringVar(&note, "note", "", "Optional closure note")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{Use: "delete <pr-id>", Short: "Delete a PR record and all retained refs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := app.NewService(".")
		if err != nil {
			return err
		}
		if err := svc.DeleteRecord(args[0]); err != nil {
			return err
		}
		cmd.Printf("Deleted PR %s\n", shortID(args[0]))
		return nil
	}}
}

func newDebugCmd() *cobra.Command {
	var which string
	var targetDir string

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug helpers for ref-backed PR data",
	}

	exportCmd := &cobra.Command{
		Use:   "export <pr-id>",
		Short: "Export a PR ref tree to a local directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(targetDir) == "" {
				return errors.New("--to is required")
			}

			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			if err := svc.DebugExport(args[0], which, targetDir); err != nil {
				return err
			}

			cmd.Printf("Exported %s for PR %s to %s\n", firstNonEmpty(which, "meta"), args[0], targetDir)
			return nil
		},
	}

	exportCmd.Flags().StringVar(&which, "ref", "meta", "Which PR ref to export: meta|head|base")
	exportCmd.Flags().StringVar(&targetDir, "to", "", "Destination directory")
	cmd.AddCommand(exportCmd)

	return cmd
}

func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the PR review TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			return tui.Run(svc)
		},
	}

	return cmd
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
