package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
)

type screen int

const (
	listScreen screen = iota
	detailScreen
)

type mode int

const (
	modeBrowse mode = iota
	modeCommentInput
	modeConfirmReject
	modeConfirmMerge
	modeConfirmCleanup
	modeInfo
)

type diffRow struct {
	FilePath     string
	IsHeader     bool
	IsComment    bool
	Header       string
	OldLine      int
	NewLine      int
	LeftText     string
	RightText    string
	LeftKind     string
	RightKind    string
	CommentCount int
	CommentKey   string
	CommentMeta  string
	CommentBody  string
	LineStart    int
	LineEnd      int
}

type prLoadedMsg struct {
	pr             model.PR
	highlightCache map[string]fileHighlight
	err            error
}

type listLoadedMsg struct {
	prs []model.PR
	err error
}

type actionResultMsg struct {
	pr      model.PR
	message string
	err     error
}

type Model struct {
	svc *app.Service

	screen screen
	mode   mode

	width  int
	height int

	openPRs                []model.PR
	listCursor             int
	currentPR              model.PR
	currentRows            []diffRow
	highlightCache         map[string]fileHighlight
	diffCursor             int
	diffOffset             int
	selectAnchor           int
	expandedComments       map[string]bool
	editingExistingComment bool
	inputBuffer            strings.Builder
	infoMessage            string
	errMessage             string
}

func Run(svc *app.Service) error {
	m := &Model{
		svc:              svc,
		screen:           listScreen,
		mode:             modeBrowse,
		expandedComments: map[string]bool{},
		selectAnchor:     -1,
	}

	// Force a color-capable renderer for the TUI even when the parent shell is
	// running with TERM=dumb/NO_COLOR. The review UI uses color as core signal.
	lipgloss.SetColorProfile(termenv.ANSI256)

	program := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithEnvironment(colorCapableEnv()),
	)
	_, err := program.Run()
	return err
}

func colorCapableEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	sawTerm := false
	sawColorTerm := false

	for _, entry := range os.Environ() {
		switch {
		case strings.HasPrefix(entry, "NO_COLOR="):
			continue
		case strings.HasPrefix(entry, "TERM="):
			env = append(env, "TERM=xterm-256color")
			sawTerm = true
		case strings.HasPrefix(entry, "COLORTERM="):
			env = append(env, "COLORTERM=truecolor")
			sawColorTerm = true
		default:
			env = append(env, entry)
		}
	}

	if !sawTerm {
		env = append(env, "TERM=xterm-256color")
	}
	if !sawColorTerm {
		env = append(env, "COLORTERM=truecolor")
	}

	return env
}

func (m *Model) Init() tea.Cmd {
	return m.loadOpenPRsCmd()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case prLoadedMsg:
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.currentPR = msg.pr
		m.highlightCache = msg.highlightCache
		m.expandedComments = map[string]bool{}
		m.rebuildRows()
		m.screen = detailScreen
		m.mode = modeBrowse
		m.diffCursor = 0
		m.diffOffset = 0
		m.selectAnchor = -1
		m.infoMessage = ""
		m.errMessage = ""
		return m, nil

	case listLoadedMsg:
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.openPRs = msg.prs
		if m.listCursor >= len(m.openPRs) && len(m.openPRs) > 0 {
			m.listCursor = len(m.openPRs) - 1
		}
		if len(m.openPRs) == 0 {
			m.listCursor = 0
		}
		m.errMessage = ""
		return m, nil

	case actionResultMsg:
		if msg.pr.ID != "" {
			m.currentPR = msg.pr
			m.rebuildRows()
		}
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.mode = modeBrowse
			if msg.pr.Status != model.StatusOpen && msg.pr.ID != "" {
				m.screen = listScreen
				return m, m.loadOpenPRsCmd()
			}
			return m, nil
		}
		m.infoMessage = msg.message
		m.errMessage = ""
		m.mode = modeBrowse
		if m.currentPR.Status != model.StatusOpen {
			m.screen = listScreen
			return m, m.loadOpenPRsCmd()
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeCommentInput:
			return m.handleCommentInput(msg)
		case modeConfirmReject:
			return m.handleConfirm(msg, "reject")
		case modeConfirmMerge:
			return m.handleConfirm(msg, "merge")
		case modeConfirmCleanup:
			return m.handleConfirm(msg, "cleanup")
		case modeInfo:
			m.mode = modeBrowse
			return m, nil
		}

		if m.screen == listScreen {
			return m.handleListKeys(msg)
		}
		return m.handleDetailKeys(msg)
	}

	return m, nil
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	switch m.screen {
	case listScreen:
		return m.renderList()
	case detailScreen:
		return m.renderDetail()
	default:
		return ""
	}
}

func (m *Model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < len(m.openPRs)-1 {
			m.listCursor++
		}
	case "enter":
		if len(m.openPRs) == 0 {
			return m, nil
		}
		pr := m.openPRs[m.listCursor]
		return m, m.loadPRCmd(pr.ID)
	}
	return m, nil
}

func (m *Model) handleDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.screen = listScreen
		m.mode = modeBrowse
		m.selectAnchor = -1
		m.errMessage = ""
		return m, m.loadOpenPRsCmd()
	case "up", "k":
		if m.diffCursor > 0 {
			m.diffCursor--
			m.adjustOffset()
		}
	case "down", "j":
		if m.diffCursor < len(m.currentRows)-1 {
			m.diffCursor++
			m.adjustOffset()
		}
	case "pgup":
		m.diffCursor -= m.visibleDiffHeight()
		if m.diffCursor < 0 {
			m.diffCursor = 0
		}
		m.adjustOffset()
	case "pgdown":
		m.diffCursor += m.visibleDiffHeight()
		if m.diffCursor >= len(m.currentRows) {
			m.diffCursor = len(m.currentRows) - 1
		}
		m.adjustOffset()
	case "v":
		if m.selectAnchor == -1 {
			m.selectAnchor = m.diffCursor
		} else {
			m.selectAnchor = -1
		}
	case "c":
		if m.currentPR.Status == model.StatusOpen {
			target, ok := m.commentTarget()
			if !ok {
				m.errMessage = "Select a diff line before commenting."
				return m, nil
			}
			m.mode = modeCommentInput
			m.inputBuffer.Reset()
			m.errMessage = ""
			m.editingExistingComment = false

			if comment, ok := m.exactCommentForTarget(target); ok {
				m.inputBuffer.WriteString(comment.Comment)
				m.infoMessage = "Editing comment: Enter adds a new line, Ctrl+S saves, Esc cancels."
				m.editingExistingComment = true
			} else {
				m.infoMessage = "New comment: Enter adds a new line, Ctrl+S saves, Esc cancels."
			}
		}
	case "o":
		if m.toggleCommentsForSelection() {
			m.rebuildRows()
		}
	case "r":
		if m.currentPR.Status == model.StatusOpen {
			m.mode = modeConfirmReject
			m.infoMessage = "Archive this PR as rejected? (y/n)"
		}
	case "m":
		if m.currentPR.Status == model.StatusOpen {
			if len(m.currentPR.MergeConflicts) > 0 {
				m.errMessage = "Merge conflicts are present. Merge is blocked."
				return m, nil
			}
			m.mode = modeConfirmMerge
			m.infoMessage = "Merge this PR into the base branch now? (y/n)"
		}
	}
	return m, nil
}

func (m *Model) handleCommentInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.infoMessage = ""
		m.editingExistingComment = false
		return m, nil
	case "backspace":
		current := []rune(m.inputBuffer.String())
		if len(current) > 0 {
			m.inputBuffer.Reset()
			m.inputBuffer.WriteString(string(current[:len(current)-1]))
		}
		return m, nil
	case "ctrl+s":
		target, ok := m.commentTarget()
		if !ok {
			m.errMessage = "Select a diff line before commenting."
			m.mode = modeBrowse
			return m, nil
		}

		comment := model.Comment{
			FilePath:  target.filePath,
			LineStart: target.lineStart,
			LineEnd:   target.lineEnd,
			Comment:   m.inputBuffer.String(),
			CommitSHA: latestCommitSHA(m.currentPR),
		}

		m.mode = modeBrowse
		m.inputBuffer.Reset()
		updated := m.editingExistingComment
		m.editingExistingComment = false
		return m, m.addCommentCmd(m.currentPR.ID, comment, updated)
	case "enter":
		m.inputBuffer.WriteString("\n")
		return m, nil
	case "space", " ":
		m.inputBuffer.WriteString(" ")
		return m, nil
	case "tab":
		m.inputBuffer.WriteString("\t")
		return m, nil
	default:
		if msg.Type == tea.KeySpace {
			m.inputBuffer.WriteString(" ")
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.inputBuffer.WriteString(string(msg.Runes))
		}
	}
	return m, nil
}

func (m *Model) handleConfirm(msg tea.KeyMsg, action string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		switch action {
		case "reject":
			return m, m.rejectCmd(m.currentPR.ID)
		case "merge":
			m.mode = modeConfirmCleanup
			m.infoMessage = "Clean up the source worktree after merge? (y/n)"
			return m, nil
		case "cleanup":
			return m, m.mergeCmd(m.currentPR.ID, true)
		}
	case "n", "esc":
		if action == "cleanup" && strings.ToLower(msg.String()) == "n" {
			return m, m.mergeCmd(m.currentPR.ID, false)
		}
		m.mode = modeBrowse
		m.infoMessage = ""
	}
	return m, nil
}

func (m *Model) renderList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var lines []string
	lines = append(lines, titleStyle.Render("Open PRs"))
	lines = append(lines, "")

	if len(m.openPRs) == 0 {
		lines = append(lines, mutedStyle.Render("No open PRs in refs/gitpr/index/open"))
	} else {
		for i, pr := range m.openPRs {
			cursor := " "
			if i == m.listCursor {
				cursor = ">"
			}
			line := fmt.Sprintf("%s %s  %-18s %s", cursor, shortID(pr.ID), pr.SourceBranch, pr.Title)
			if i == m.listCursor {
				line = cursorStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("Keys: j/k move  enter open  q quit"))
	if m.errMessage != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.errMessage))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderDetail() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	conflictStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("236"))
	selectionStyle := lipgloss.NewStyle().Background(lipgloss.Color("238"))

	header := []string{
		titleStyle.Render(fmt.Sprintf("PR %s: %s", shortID(m.currentPR.ID), m.currentPR.Title)),
		fmt.Sprintf("Branch: %s  Base: %s  Status: %s", m.currentPR.SourceBranch, m.currentPR.BaseBranch, m.currentPR.Status),
		fmt.Sprintf("Source SHA: %s  Base SHA: %s", shortID(m.currentPR.SourceHeadSHA), shortID(m.currentPR.BaseHeadSHA)),
		fmt.Sprintf("Worktree: %s", m.currentPR.SourceWorktreePath),
	}

	if m.currentPR.Description != "" {
		header = append(header, "Description: "+m.currentPR.Description)
	}
	if len(m.currentPR.MergeConflicts) > 0 {
		header = append(header, conflictStyle.Render(fmt.Sprintf("Merge blocked: %d conflict(s)", len(m.currentPR.MergeConflicts))))
		for _, conflict := range m.currentPR.MergeConflicts {
			header = append(header, conflictStyle.Render("  "+conflict.Message))
		}
	}
	header = append(header, "")

	visibleStart, visibleEnd := m.visibleRange()
	rows := make([]string, 0, visibleEnd-visibleStart)
	for idx := visibleStart; idx < visibleEnd; idx++ {
		row := m.currentRows[idx]
		text := renderRow(m.width, row)
		if row.IsHeader {
			text = titleStyle.Render(text)
		}
		if idx == m.diffCursor {
			text = cursorStyle.Render(text)
		} else if m.isSelected(idx) {
			text = selectionStyle.Render(text)
		}
		rows = append(rows, text)
	}

	footer := []string{
		"",
		mutedStyle.Render("Keys: j/k move  v select block  c comment/edit  o toggle comments  r request-changes  m merge  esc back  q quit"),
		mutedStyle.Render(fmt.Sprintf("Comments: %d  Commits: %d", len(m.currentPR.Comments), len(m.currentPR.Commits))),
	}

	if m.mode == modeCommentInput {
		commentStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("117")).
			Padding(0, 1)
		footer = append(footer, mutedStyle.Render("Comment editor: Ctrl+S save, Enter newline, Esc cancel"))
		footer = append(footer, commentStyle.Render(commentEditorContent(m.inputBuffer.String())))
	}
	if m.infoMessage != "" {
		footer = append(footer, lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render(m.infoMessage))
	}
	if m.errMessage != "" {
		footer = append(footer, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.errMessage))
	}

	return strings.Join(append(append(header, rows...), footer...), "\n")
}

func (m *Model) loadOpenPRsCmd() tea.Cmd {
	return func() tea.Msg {
		prs, err := m.svc.OpenPRs()
		if err != nil {
			return listLoadedMsg{err: err}
		}
		return listLoadedMsg{prs: prs}
	}
}

func (m *Model) loadPRCmd(id string) tea.Cmd {
	return func() tea.Msg {
		pr, _, err := m.svc.LoadPR(id)
		if err != nil {
			return prLoadedMsg{err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pr, err = m.svc.RefreshConflicts(ctx, pr)
		if err != nil {
			return prLoadedMsg{err: err}
		}

		cache := buildHighlightCache(ctx, pr)
		return prLoadedMsg{pr: pr, highlightCache: cache}
	}
}

func (m *Model) addCommentCmd(id string, comment model.Comment, updated bool) tea.Cmd {
	return func() tea.Msg {
		pr, err := m.svc.AddComment(id, comment)
		if err != nil {
			return actionResultMsg{err: err}
		}
		action := "Saved"
		if updated {
			action = "Updated"
		}
		return actionResultMsg{
			pr:      pr,
			message: fmt.Sprintf("%s comment on %s:%d-%d", action, comment.FilePath, comment.LineStart, comment.LineEnd),
		}
	}
}

func (m *Model) rejectCmd(id string) tea.Cmd {
	return func() tea.Msg {
		pr, _, err := m.svc.RejectPR(id)
		if err != nil {
			return actionResultMsg{err: err}
		}
		return actionResultMsg{
			pr:      pr,
			message: fmt.Sprintf("PR %s marked as rejected", shortID(pr.ID)),
		}
	}
}

func (m *Model) mergeCmd(id string, cleanup bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		pr, _, err := m.svc.MergePR(ctx, id, cleanup)
		if err != nil {
			return actionResultMsg{err: err}
		}
		msg := fmt.Sprintf("PR %s merged into %s", shortID(pr.ID), pr.BaseBranch)
		if !cleanup {
			msg += ". Source worktree kept."
		}
		return actionResultMsg{pr: pr, message: msg}
	}
}

func (m *Model) visibleDiffHeight() int {
	height := m.height - 11
	if height < 5 {
		return 5
	}
	return height
}

func (m *Model) visibleRange() (int, int) {
	start := m.diffOffset
	end := start + m.visibleDiffHeight()
	if end > len(m.currentRows) {
		end = len(m.currentRows)
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

func (m *Model) adjustOffset() {
	height := m.visibleDiffHeight()
	if m.diffCursor < m.diffOffset {
		m.diffOffset = m.diffCursor
	}
	if m.diffCursor >= m.diffOffset+height {
		m.diffOffset = m.diffCursor - height + 1
	}
	if m.diffOffset < 0 {
		m.diffOffset = 0
	}
}

func (m *Model) isSelected(idx int) bool {
	if m.selectAnchor == -1 {
		return false
	}
	start, end := sortedPair(m.selectAnchor, m.diffCursor)
	return idx >= start && idx <= end
}

func (m *Model) rebuildRows() {
	m.currentRows = buildRows(m.currentPR, m.expandedComments, m.highlightCache)
}

type commentTarget struct {
	filePath  string
	lineStart int
	lineEnd   int
}

func (m *Model) commentTarget() (commentTarget, bool) {
	if len(m.currentRows) == 0 {
		return commentTarget{}, false
	}

	start, end := m.diffCursor, m.diffCursor
	if m.selectAnchor != -1 {
		start, end = sortedPair(m.selectAnchor, m.diffCursor)
	}

	var target commentTarget
	for idx := start; idx <= end; idx++ {
		row := m.currentRows[idx]
		if row.IsHeader || row.IsComment || row.FilePath == "" {
			continue
		}
		line := rowLine(row)
		if line == 0 {
			continue
		}
		if target.filePath == "" {
			target.filePath = row.FilePath
			target.lineStart = line
			target.lineEnd = line
			continue
		}
		if target.filePath != row.FilePath {
			break
		}
		if line < target.lineStart {
			target.lineStart = line
		}
		if line > target.lineEnd {
			target.lineEnd = line
		}
	}

	return target, target.filePath != ""
}

func (m *Model) exactCommentForTarget(target commentTarget) (model.Comment, bool) {
	for _, comment := range m.currentPR.Comments {
		if comment.FilePath == target.filePath && comment.LineStart == target.lineStart && comment.LineEnd == target.lineEnd {
			return comment, true
		}
	}
	return model.Comment{}, false
}

func (m *Model) toggleCommentsForSelection() bool {
	target, ok := m.commentTarget()
	if !ok {
		m.errMessage = "Select a diff line with comments to expand or collapse them."
		return false
	}

	keys := m.commentKeysForTarget(target)
	if len(keys) == 0 {
		m.errMessage = "No comments attached to the selected line range."
		return false
	}

	expand := false
	for _, key := range keys {
		if !m.expandedComments[key] {
			expand = true
			break
		}
	}

	for _, key := range keys {
		m.expandedComments[key] = expand
		if !expand {
			delete(m.expandedComments, key)
		}
	}

	if expand {
		m.infoMessage = "Expanded inline comments."
	} else {
		m.infoMessage = "Collapsed inline comments."
	}
	m.errMessage = ""
	return true
}

func (m *Model) commentKeysForTarget(target commentTarget) []string {
	var keys []string
	for idx, comment := range m.currentPR.Comments {
		if comment.FilePath != target.filePath {
			continue
		}
		if comment.LineStart > target.lineEnd || comment.LineEnd < target.lineStart {
			continue
		}
		keys = append(keys, commentKey(comment, idx))
	}
	return keys
}

func buildRows(pr model.PR, expandedComments map[string]bool, highlightCache map[string]fileHighlight) []diffRow {
	commentIndex := buildCommentIndex(pr.Comments)
	var rows []diffRow
	for _, file := range pr.FileDiffs {
		filePath := file.NewPath
		if filePath == "" {
			filePath = file.OldPath
		}
		highlight := highlightCache[filePath]

		rows = append(rows, diffRow{
			IsHeader: true,
			Header:   fmt.Sprintf("%s (%s)", filePath, file.Status),
		})

		for _, hunk := range file.Hunks {
			if hunk.Header != "" {
				rows = append(rows, diffRow{
					IsHeader: true,
					Header:   "@@ " + hunk.Header,
				})
			}

			for _, row := range buildHunkRows(filePath, hunk, highlight) {
				line := rowLine(row)
				anchored, count := commentsForLine(commentIndex[filePath], line)
				row.CommentCount = count
				rows = append(rows, row)
				for _, comment := range anchored {
					if !expandedComments[comment.key] {
						continue
					}
					rows = append(rows, diffRow{
						FilePath:    filePath,
						IsComment:   true,
						CommentKey:  comment.key,
						CommentMeta: commentSummary(comment.comment),
						CommentBody: comment.comment.Comment,
						LineStart:   comment.comment.LineStart,
						LineEnd:     comment.comment.LineEnd,
					})
				}
			}
		}
	}
	return rows
}

func buildHunkRows(filePath string, hunk model.Hunk, highlight fileHighlight) []diffRow {
	var rows []diffRow
	lines := hunk.Lines
	for i := 0; i < len(lines); {
		switch lines[i].Kind {
		case "context":
			oldHighlighted, _ := highlightedLine(highlight.old, lines[i].OldLine)
			newHighlighted, _ := highlightedLine(highlight.new, lines[i].NewLine)
			rows = append(rows, diffRow{
				FilePath:  filePath,
				OldLine:   lines[i].OldLine,
				NewLine:   lines[i].NewLine,
				LeftKind:  "context",
				RightKind: "context",
				LeftText:  renderDiffCell("context", "  ", oldHighlighted, lines[i].Content),
				RightText: renderDiffCell("context", "  ", newHighlighted, lines[i].Content),
			})
			i++
		default:
			start := i
			for i < len(lines) && lines[i].Kind != "context" {
				i++
			}
			block := lines[start:i]
			var deletes []model.DiffLine
			var adds []model.DiffLine
			for _, line := range block {
				if line.Kind == "delete" {
					deletes = append(deletes, line)
				}
				if line.Kind == "add" {
					adds = append(adds, line)
				}
			}
			rows = append(rows, pairChangedRows(filePath, deletes, adds, highlight)...)
		}
	}
	return rows
}

func pairChangedRows(filePath string, deletes, adds []model.DiffLine, highlight fileHighlight) []diffRow {
	size := len(deletes)
	if len(adds) > size {
		size = len(adds)
	}

	rows := make([]diffRow, 0, size)
	for i := 0; i < size; i++ {
		row := diffRow{FilePath: filePath}
		if i < len(deletes) {
			highlighted, _ := highlightedLine(highlight.old, deletes[i].OldLine)
			row.OldLine = deletes[i].OldLine
			row.LeftKind = "delete"
			row.LeftText = renderDiffCell("delete", "- ", highlighted, deletes[i].Content)
		}
		if i < len(adds) {
			highlighted, _ := highlightedLine(highlight.new, adds[i].NewLine)
			row.NewLine = adds[i].NewLine
			row.RightKind = "add"
			row.RightText = renderDiffCell("add", "+ ", highlighted, adds[i].Content)
		}
		rows = append(rows, row)
	}
	return rows
}

func renderRow(totalWidth int, row diffRow) string {
	if row.IsHeader {
		return row.Header
	}
	if row.IsComment {
		return renderCommentRow(totalWidth, row)
	}

	leftWidth := max(20, (totalWidth-11)/2)
	rightWidth := max(20, totalWidth-11-leftWidth)

	leftLine := "    "
	rightLine := "    "
	if row.OldLine > 0 {
		leftLine = formatLineNumber(row.LeftKind, row.OldLine, row.CommentCount > 0 && row.NewLine == 0)
	}
	if row.NewLine > 0 {
		rightLine = formatLineNumber(row.RightKind, row.NewLine, row.CommentCount > 0)
	}

	left := ansi.Truncate(row.LeftText, leftWidth, "...")
	right := ansi.Truncate(row.RightText, rightWidth, "...")
	return fmt.Sprintf("%s %s | %s %s", leftLine, padANSI(left, leftWidth), rightLine, padANSI(right, rightWidth))
}

func latestCommitSHA(pr model.PR) string {
	if len(pr.Commits) == 0 {
		return ""
	}
	return pr.Commits[0].SHA
}

func sortedPair(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func commentEditorContent(content string) string {
	if content == "" {
		return " "
	}
	return content
}

type indexedComment struct {
	key     string
	comment model.Comment
}

func buildCommentIndex(comments []model.Comment) map[string][]indexedComment {
	index := map[string][]indexedComment{}
	for idx, comment := range comments {
		index[comment.FilePath] = append(index[comment.FilePath], indexedComment{
			key:     commentKey(comment, idx),
			comment: comment,
		})
	}
	return index
}

func commentsForLine(comments []indexedComment, line int) ([]indexedComment, int) {
	if line == 0 {
		return nil, 0
	}

	var anchored []indexedComment
	count := 0
	for _, comment := range comments {
		if line < comment.comment.LineStart || line > comment.comment.LineEnd {
			continue
		}
		count++
		if line == comment.comment.LineEnd {
			anchored = append(anchored, comment)
		}
	}
	return anchored, count
}

func commentKey(comment model.Comment, idx int) string {
	return fmt.Sprintf("%s:%d:%d:%d", comment.FilePath, comment.LineStart, comment.LineEnd, idx)
}

func rowLine(row diffRow) int {
	if row.NewLine > 0 {
		return row.NewLine
	}
	return row.OldLine
}

func renderCommentRow(totalWidth int, row diffRow) string {
	width := max(30, totalWidth-2)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("221")).
		Padding(0, 1).
		Width(width)
	body := commentMetaStyle.Render(row.CommentMeta) + "\n" + commentBodyStyle.Render(row.CommentBody)
	return boxStyle.Render(body)
}

func formatLineNumber(kind string, line int, hasComments bool) string {
	if hasComments {
		return commentBadgeStyle.Render(fmt.Sprintf("%3d*", line))
	}
	switch kind {
	case "add":
		return diffAddStyle.Render(fmt.Sprintf("%4d", line))
	case "delete":
		return diffDeleteStyle.Render(fmt.Sprintf("%4d", line))
	default:
		return diffContextStyle.Render(fmt.Sprintf("%4d", line))
	}
}

func padANSI(text string, width int) string {
	padding := width - lipgloss.Width(text)
	if padding < 0 {
		padding = 0
	}
	return text + strings.Repeat(" ", padding)
}

func commentSummary(comment model.Comment) string {
	summary := fmt.Sprintf("Comment %d-%d", comment.LineStart, comment.LineEnd)
	if comment.CommitSHA != "" {
		summary += " @ " + shortID(comment.CommitSHA)
	}
	return summary
}
