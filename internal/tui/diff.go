// relationships:
//
//	implements: adopt-branch-based-pull-requests
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
)

type reviewLoadedMsg struct {
	id     string
	report app.ReviewReport
	err    error
}

func (m *Model) loadReviewCmd(id string) tea.Cmd {
	return func() tea.Msg {
		report, err := m.svc.ReviewPR(context.Background(), id)
		return reviewLoadedMsg{id: id, report: report, err: err}
	}
}

func (m *Model) handleDiffKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = detailScreen
		m.errMessage = ""
	case "j", "down":
		m.diffOffset++
	case "k", "up":
		m.diffOffset--
	case "pgdown", " ":
		m.diffOffset += m.diffPageSize()
	case "pgup":
		m.diffOffset -= m.diffPageSize()
	case "home", "g":
		m.diffOffset = 0
	case "end", "G":
		m.diffOffset = len(m.diffLines())
	case "R":
		if m.currentPR != nil {
			return m, m.loadReviewCmd(m.currentPR.ID)
		}
	case "c", "r", "m":
		return m.handleDetailKeys(msg)
	}
	m.clampDiffOffset(len(m.diffLines()))
	return m, nil
}

func (m *Model) diffPageSize() int { return max(1, m.height-4) }

func (m *Model) clampDiffOffset(count int) {
	m.diffOffset = max(0, min(m.diffOffset, max(0, count-m.diffPageSize())))
}

func (m *Model) diffLines() []string {
	content := renderReview(m.review)
	if m.width > 0 {
		content = ansi.Hardwrap(content, m.width, true)
	}
	return strings.Split(content, "\n")
}

func (m *Model) renderDiff() string {
	lines := m.diffLines()
	m.clampDiffOffset(len(lines))
	end := min(len(lines), m.diffOffset+m.diffPageSize())
	footer := []string{
		fmt.Sprintf("Read-only diff (%d-%d/%d). j/k scroll  PgUp/PgDn page", m.diffOffset+1, end, len(lines)),
		"g/G top/end  R refresh  c/r/m CLI guidance  esc back  q quit",
		strings.ReplaceAll(m.infoMessage, "\n", " "),
		strings.ReplaceAll(m.errMessage, "\n", " "),
	}
	if m.width > 0 {
		for i := range footer {
			footer[i] = ansi.Truncate(footer[i], m.width, "…")
		}
	}
	return strings.Join(append(lines[m.diffOffset:end:end], footer...), "\n")
}

// renderReview uses only the live report: stored thread positions can differ
// from the remapped anchors and outdated state in this report.
func renderReview(report *app.ReviewReport) string {
	if report == nil {
		return "No review report loaded."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PR %s: %s -> %s\nReview basis\nSource head: %s\nBase head: %s\nMerge base: %s\n", shortID(report.ID), report.SourceBranch, report.BaseBranch, report.Basis.SourceHeadSHA, report.Basis.BaseHeadSHA, report.Basis.MergeBaseSHA)
	if report.Basis.BaseContained != nil {
		fmt.Fprintf(&b, "Base contained in source: %t\n", *report.Basis.BaseContained)
	}
	missing := report.Basis.SourceBranchMissing || report.Basis.BaseBranchMissing
	if report.Basis.SourceBranchMissing {
		b.WriteString("Source branch missing.\n")
	}
	if report.Basis.BaseBranchMissing {
		b.WriteString("Base branch missing.\n")
	}
	if missing {
		b.WriteString("Live diff and remapped threads unavailable. Return to detail for stored thread counts.\n")
	} else if len(report.Diff) == 0 {
		b.WriteString("No changed files.\n")
	}
	for _, file := range report.Diff {
		fmt.Fprintf(&b, "\nFile: %s -> %s [%s]\n", file.OldPath, file.NewPath, file.Status)
		if len(file.Hunks) == 0 {
			if file.Patch != "" {
				b.WriteString(file.Patch)
				b.WriteByte('\n')
			} else {
				b.WriteString("No text hunks (binary or metadata-only change).\n")
			}
		}
		for _, hunk := range file.Hunks {
			fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@ %s\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines, hunk.Header)
			for _, line := range hunk.Lines {
				prefix := " "
				switch line.Kind {
				case "add":
					prefix = "+"
				case "delete":
					prefix = "-"
				}
				old, new := "", ""
				if line.OldLine > 0 {
					old = fmt.Sprint(line.OldLine)
				}
				if line.NewLine > 0 {
					new = fmt.Sprint(line.NewLine)
				}
				fmt.Fprintf(&b, "%5s %5s %s%s\n", old, new, prefix, line.Content)
			}
		}
	}
	if !missing && len(report.Threads) == 0 {
		b.WriteString("\nNo threads.\n")
	}
	for _, thread := range report.Threads {
		renderThread(&b, thread)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func renderThread(b *strings.Builder, thread model.Thread) {
	fmt.Fprintf(b, "\nThread %s [%s, %s, outdated: %t]\n", thread.ID, thread.Kind, thread.Status, thread.Outdated)
	if anchor := thread.Anchor; anchor != nil {
		fmt.Fprintf(b, "Anchor: %s %s:%d-%d\nAnchor basis: %s / %s\n", anchor.File, anchor.Side, anchor.LineStart, anchor.LineEnd, anchor.SourceHeadSHA, anchor.BaseHeadSHA)
	}
	for _, comment := range thread.Comments {
		fmt.Fprintf(b, "Comment (post-closure: %t):\n%s\n", comment.PostClosure, comment.Body)
	}
}
