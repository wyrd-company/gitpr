package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		Use:           "gitpr",
		Short:         "Review local git worktree branches as lightweight PRs",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newCreateCmd())
	rootCmd.AddCommand(newEditCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newReviewCmd())
	rootCmd.AddCommand(newApproveCmd())
	rootCmd.AddCommand(newCommentsCmd())
	rootCmd.AddCommand(newCommentCmd())
	rootCmd.AddCommand(newThreadStatusCmd("resolve", model.ThreadResolved))
	rootCmd.AddCommand(newThreadStatusCmd("reopen", model.ThreadOpen))
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
	var descriptionFile string
	var worktree string
	var base string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a branch-based PR from a local worktree branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceRoot := "."
			if strings.TrimSpace(worktree) != "" {
				serviceRoot = worktree
			}

			desc, err := resolveDescription(cmd, description, descriptionFile)
			if err != nil {
				return err
			}

			svc, err := app.NewService(serviceRoot)
			if err != nil {
				return err
			}

			req := app.CreatePRRequest{
				Title:       title,
				Description: desc,
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
	cmd.Flags().StringVarP(&description, "description", "d", "", "PR description (shell-quoting fragile for multiline/backtick content; prefer --description-file)")
	cmd.Flags().StringVar(&descriptionFile, "description-file", "", "Read the PR description verbatim from a file, or - for standard input")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Source worktree path (defaults to current directory)")
	cmd.Flags().StringVar(&base, "base", "", "Override detected default branch")
	_ = cmd.MarkFlagRequired("title")
	cmd.MarkFlagsMutuallyExclusive("description", "description-file")

	return cmd
}

// resolveDescription returns the description content for create/edit
// commands. --description-file (or "-" for standard input) is read
// verbatim, byte-for-byte, with no trimming or shell interpretation.
// --description is used as passed by cobra. Neither flag set yields "".
// A --description-file flag that was set but given a blank (or
// whitespace-only) path is rejected explicitly: silently falling back to
// "no file" would store an empty description with exit 0, the exact
// silently-empty shape (549/711) this flag exists to prevent.
func resolveDescription(cmd *cobra.Command, description, descriptionFile string) (string, error) {
	if !cmd.Flags().Changed("description-file") {
		return description, nil
	}
	if strings.TrimSpace(descriptionFile) == "" {
		return "", errors.New("--description-file requires a non-blank path (or - for standard input)")
	}
	if descriptionFile == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("reading description from standard input: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(descriptionFile)
	if err != nil {
		return "", fmt.Errorf("reading --description-file %q: %w", descriptionFile, err)
	}
	return string(data), nil
}

func newEditCmd() *cobra.Command {
	var title string
	var description string
	var descriptionFile string

	cmd := &cobra.Command{
		Use:   "edit <pr-id>",
		Short: "Replace title and/or description on an open branch-based PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("title") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("description-file") {
				return errors.New("edit requires --title, --description, or --description-file")
			}

			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			req := app.EditPRRequest{}
			if cmd.Flags().Changed("title") {
				req.Title = &title
			}
			if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-file") {
				desc, err := resolveDescription(cmd, description, descriptionFile)
				if err != nil {
					return err
				}
				req.Description = &desc
			}

			pr, ref, err := svc.EditPR(args[0], req)
			if err != nil {
				return err
			}

			cmd.Printf("Edited PR %s at %s\n", shortID(pr.ID), ref)
			return nil
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "New PR title")
	cmd.Flags().StringVarP(&description, "description", "d", "", "New PR description (shell-quoting fragile for multiline/backtick content; prefer --description-file)")
	cmd.Flags().StringVar(&descriptionFile, "description-file", "", "Read the new PR description verbatim from a file, or - for standard input")
	cmd.MarkFlagsMutuallyExclusive("description", "description-file")

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
			if reason != "" && filter != "closed" {
				flag := "--state"
				if cmd.Flags().Changed("status") {
					flag = "--status"
				}
				return fmt.Errorf("--reason can be combined only with %s closed", flag)
			}
			prs, skipped, err := svc.ListPRsWithReason(filter, model.ClosureReason(reason))
			if err != nil {
				return err
			}

			if len(prs) == 0 {
				cmd.Println("No PRs found.")
				if skipped > 0 {
					cmd.Println(app.SkippedRecordsMessage(skipped))
				}
				return nil
			}

			cmd.Printf("%-14s %-10s %-20s %s\n", "ID", "STATUS", "BRANCH", "TITLE")
			for _, pr := range prs {
				suffix := ""
				if pr.State == model.PRStateClosed && pr.Closure != nil {
					suffix = " (" + string(pr.Closure.Reason) + ")"
				}
				cmd.Printf("%-14s %-10s %-20s %s%s\n", shortID(pr.ID), pr.State, pr.SourceBranch, pr.Title, suffix)
			}
			if skipped > 0 {
				cmd.Println(app.SkippedRecordsMessage(skipped))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&state, "state", "open", "Filter by state: open|merged|closed")
	cmd.Flags().StringVar(&status, "status", "", "Deprecated alias for --state")
	cmd.Flags().StringVar(&reason, "reason", "", "Filter closed PRs by reason")
	cmd.Flags().BoolVar(&all, "all", false, "Show records in every state")
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
				prs, _, err := svc.ListPRs(status)
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

			type pr2YAML model.PR2
			summary := struct {
				Open     int `yaml:"open"`
				Resolved int `yaml:"resolved"`
				Outdated int `yaml:"outdated"`
			}{}
			for _, thread := range pr.Threads {
				if thread.Status == model.ThreadResolved {
					summary.Resolved++
				} else {
					summary.Open++
				}
				if thread.Outdated {
					summary.Outdated++
				}
			}
			shown := struct {
				PR            pr2YAML `yaml:",inline"`
				ThreadSummary any     `yaml:"thread_summary"`
			}{pr2YAML(pr), summary}
			out, err := yaml.Marshal(shown)
			if err != nil {
				return err
			}

			cmd.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by state when no PR ID is provided: open|merged|closed|all")
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
				prs, _, err := svc.ListPRs(status)
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

			pr, _, err := svc.LoadRecord(targetID)
			if err != nil {
				return err
			}
			payload := struct {
				ID      string         `yaml:"id"`
				Title   string         `yaml:"title"`
				State   model.PRState  `yaml:"state"`
				Threads []model.Thread `yaml:"threads"`
			}{pr.ID, pr.Title, pr.State, pr.Threads}

			out, err := yaml.Marshal(payload)
			if err != nil {
				return err
			}

			cmd.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by state when no PR ID is provided: open|merged|closed|all")
	return cmd
}

func newCommentCmd() *cobra.Command {
	var filePath string
	var lineStart int
	var lineEnd int
	var text string
	var prLevel bool
	var threadID, side string
	var headFlags expectedHeadFlags

	cmd := &cobra.Command{
		Use:   "comment <pr-id>",
		Short: "Add a comment to a PR thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}
			if prLevel {
				for _, name := range []string{"file", "line-start", "line-end", "side"} {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s cannot be used with --pr-level", name)
					}
				}
			}
			heads, err := headFlags.parse()
			if err != nil {
				return err
			}
			var diffSide model.DiffSide
			switch side {
			case "", "new":
				diffSide = model.DiffSideSource
			case "old":
				diffSide = model.DiffSideBase
			default:
				return errors.New("--side must be old or new")
			}
			pr, _, err := svc.CommentPR2(cmd.Context(), args[0], app.ThreadCommentRequest{ThreadID: threadID, PRLevel: prLevel, File: filePath, Side: diffSide, LineStart: lineStart, LineEnd: lineEnd, Text: text, Heads: heads})
			if err != nil {
				return err
			}
			createdID := threadID
			if createdID == "" {
				createdID = pr.Threads[len(pr.Threads)-1].ID
			}
			cmd.Printf("Saved comment in thread %s on PR %s\n", createdID, shortID(pr.ID))
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Changed file path for the comment")
	cmd.Flags().IntVar(&lineStart, "line-start", 0, "Starting line number")
	cmd.Flags().IntVar(&lineEnd, "line-end", 0, "Ending line number (defaults to line-start)")
	cmd.Flags().StringVar(&text, "text", "", "Comment text")
	cmd.Flags().BoolVar(&prLevel, "pr-level", false, "Create a PR-level thread")
	cmd.Flags().StringVar(&threadID, "thread", "", "Reply in an existing thread")
	cmd.Flags().StringVar(&side, "side", "new", "Anchored diff side: old|new")
	headFlags.bind(cmd)

	return cmd
}

func newThreadStatusCmd(verb string, status model.ThreadStatus) *cobra.Command {
	return &cobra.Command{Use: verb + " <pr-id> <thread-id>", Short: strings.ToUpper(verb[:1]) + verb[1:] + " a PR comment thread", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := app.NewService(".")
		if err != nil {
			return err
		}
		pr, _, err := svc.SetThreadStatus(args[0], args[1], status)
		if err != nil {
			return err
		}
		label := strings.ToUpper(verb[:1]) + verb[1:]
		cmd.Printf("%s thread %s on PR %s\n", label, args[1], shortID(pr.ID))
		return nil
	}}
}

func newRejectCmd() *cobra.Command {
	var flags expectedHeadFlags
	cmd := &cobra.Command{
		Use:     "reject <pr-id>",
		Aliases: []string{"request-changes"},
		Short:   "Record a rejected review event",
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
			pr, ref, err := svc.RejectPR(cmd.Context(), args[0], heads)
			if err != nil {
				return err
			}

			cmd.Printf("Rejected PR %s at %s\n", shortID(pr.ID), ref)
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
		Short: "Merge an eligible PR into its base branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			pr, ref, err := svc.MergePR(ctx, args[0], cleanup)
			if err != nil {
				if ref != "" {
					printMergeSuccess(cmd, pr, ref, cleanup)
				}
				return err
			}

			printMergeSuccess(cmd, pr, ref, cleanup)
			return nil
		},
	}

	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "Remove the source worktree and branch after a successful merge")
	return cmd
}

func printMergeSuccess(cmd *cobra.Command, pr model.PR2, ref string, cleanup bool) {
	cmd.Printf("Merged PR %s into %s at %s\n", shortID(pr.ID), pr.BaseBranch, ref)
	if !cleanup {
		cmd.Println("Source worktree kept.")
	}
}

func newCloseCmd() *cobra.Command {
	var reason, destination, supersededBy, note string
	var commits, patchIDs []string
	cmd := &cobra.Command{Use: "close <pr-id>", Short: "Close an open PR", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	var force bool
	cmd := &cobra.Command{Use: "delete <pr-id>", Short: "Delete a PR record and all retained refs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := app.NewService(".")
		if err != nil {
			return err
		}
		if !force {
			summary, err := svc.DeleteRecordSummary(args[0])
			if err != nil {
				return err
			}
			cmd.Printf("Would delete PR %s (state: %s, events: %d, threads: %d).\n", summary.ID, summary.State, summary.EventCount, summary.ThreadCount)
			cmd.Println("Warning: pinned review commits may become collectable. Re-run with --force to delete.")
			return errors.New("refusing to delete without --force")
		}
		if err := svc.DeleteRecord(args[0]); err != nil {
			return err
		}
		cmd.Printf("Deleted PR %s\n", shortID(args[0]))
		return nil
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Permanently delete the record and retained refs")
	return cmd
}

func newDebugCmd() *cobra.Command {
	var targetDir string

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug helpers for ref-backed PR data",
	}

	exportCmd := &cobra.Command{
		Use:   "export <pr-id>",
		Short: "Export a PR metadata ref tree to a local directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(targetDir) == "" {
				return errors.New("--to is required")
			}

			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			if err := svc.DebugExport(args[0], targetDir); err != nil {
				return err
			}

			cmd.Printf("Exported meta for PR %s to %s\n", args[0], targetDir)
			return nil
		},
	}

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
