package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/tui"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "gitpr",
		Short:   "Review local git worktree branches as lightweight PRs",
		Version: version,
	}

	rootCmd.AddCommand(newCreateCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newCommentsCmd())
	rootCmd.AddCommand(newCommentCmd())
	rootCmd.AddCommand(newDebugCmd())
	rootCmd.AddCommand(newTUICmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

			fmt.Printf("Created PR %s at %s\n", pr.ID, ref)
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
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(".")
			if err != nil {
				return err
			}

			prs, err := svc.ListPRs(status)
			if err != nil {
				return err
			}

			if len(prs) == 0 {
				fmt.Println("No PRs found.")
				return nil
			}

			fmt.Printf("%-14s %-10s %-20s %s\n", "ID", "STATUS", "BRANCH", "TITLE")
			for _, pr := range prs {
				fmt.Printf("%-14s %-10s %-20s %s\n", shortID(pr.ID), pr.Status, pr.SourceBranch, pr.Title)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "open", "Filter by status: open|approved|rejected|closed|all")
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

			pr, _, err := svc.LoadPR(targetID)
			if err != nil {
				return err
			}

			out, err := yaml.Marshal(pr)
			if err != nil {
				return err
			}

			fmt.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by status when no PR ID is provided: open|approved|rejected|closed|all")
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

			pr, _, err := svc.LoadPR(targetID)
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

			fmt.Print(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "all", "Filter by status when no PR ID is provided: open|approved|rejected|closed|all")
	return cmd
}

func newCommentCmd() *cobra.Command {
	var filePath string
	var lineStart int
	var lineEnd int
	var text string
	var commitSHA string

	cmd := &cobra.Command{
		Use:   "comment <pr-id>",
		Short: "Add a review comment to an open PR",
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

			pr, err := svc.AddComment(args[0], comment)
			if err != nil {
				return err
			}

			fmt.Printf("Saved comment on PR %s: %s:%d-%d\n", shortID(pr.ID), comment.FilePath, comment.LineStart, comment.LineEnd)
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Changed file path for the comment")
	cmd.Flags().IntVar(&lineStart, "line-start", 0, "Starting line number")
	cmd.Flags().IntVar(&lineEnd, "line-end", 0, "Ending line number (defaults to line-start)")
	cmd.Flags().StringVar(&text, "text", "", "Comment text")
	cmd.Flags().StringVar(&commitSHA, "commit", "", "Optional commit SHA")

	return cmd
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

			fmt.Printf("Exported %s for PR %s to %s\n", firstNonEmpty(which, "meta"), args[0], targetDir)
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
