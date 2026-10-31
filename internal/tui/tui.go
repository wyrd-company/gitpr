package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
)

type screen int

const (
	listScreen screen = iota
	detailScreen
	diffScreen
)

type prLoadedMsg struct {
	record model.PR2
	err    error
}

type listLoadedMsg struct {
	records []model.PR2
	skipped int
	err     error
}

type Model struct {
	svc *app.Service

	screen screen

	width  int
	height int

	openRecords   []model.PR2
	allRecords    []model.PR2
	skipped       int
	showAll       bool
	listCursor    int
	currentPR     *model.PR2
	infoMessage   string
	errMessage    string
	review        *app.ReviewReport
	diffOffset    int
	reviewRequest *reviewRequest
}

func Run(svc *app.Service) error {
	m := &Model{svc: svc, screen: listScreen}

	program := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)
	_, err := program.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return m.loadPRsCmd()
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
		m.reviewRequest = nil
		record := msg.record
		m.currentPR = &record
		m.screen = detailScreen
		m.infoMessage = ""
		m.errMessage = ""
		return m, nil

	case reviewLoadedMsg:
		if msg.request == nil || msg.request != m.reviewRequest || m.currentPR == nil || m.currentPR.ID != msg.id || m.screen != msg.request.screen {
			return m, nil
		}
		m.reviewRequest = nil
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.review = &msg.report
		m.diffOffset = 0
		m.screen = diffScreen
		m.errMessage, m.infoMessage = "", ""
		return m, nil

	case listLoadedMsg:
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.allRecords = msg.records
		m.skipped = msg.skipped
		m.applyListMode()
		m.errMessage = ""
		m.infoMessage = ""
		return m, nil

	case tea.KeyMsg:
		if m.screen == listScreen {
			return m.handleListKeys(msg)
		}
		if m.screen == diffScreen {
			return m.handleDiffKeys(msg)
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
	case diffScreen:
		return m.renderDiff()
	default:
		return ""
	}
}

func (m *Model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.reviewRequest = nil
		return m, tea.Quit
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < len(m.openRecords)-1 {
			m.listCursor++
		}
	case "enter":
		if len(m.openRecords) == 0 {
			return m, nil
		}
		return m, m.loadPRCmd(m.openRecords[m.listCursor].ID)
	case "a":
		m.showAll = !m.showAll
		m.applyListMode()
	}
	return m, nil
}

func (m *Model) handleDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.reviewRequest = nil
		return m, tea.Quit
	case "esc":
		m.reviewRequest = nil
		m.screen, m.currentPR, m.errMessage = listScreen, nil, ""
		return m, m.loadPRsCmd()
	case "d":
		if m.currentPR != nil {
			return m, m.loadReviewCmd(m.currentPR.ID)
		}
	case "c", "r", "m":
		m.infoMessage = "PR actions use the CLI: gitpr review <id>, approve/reject with its basis, gitpr merge <id>, or gitpr comment <id>."
	}
	return m, nil
}

func (m *Model) renderList() string {
	var lines []string
	title := "Open PRs"
	if m.showAll {
		title = "All PRs"
	}
	lines = append(lines, titleStyle.Render(title), "")

	if len(m.openRecords) == 0 {
		lines = append(lines, mutedStyle.Render("No open PRs in refs/gitpr/index/open"))
	} else {
		for i, pr := range m.openRecords {
			cursor := " "
			if i == m.listCursor {
				cursor = ">"
			}
			state := string(pr.State)
			if pr.State == model.PRStateClosed && pr.Closure != nil {
				state += " (" + string(pr.Closure.Reason) + ")"
			}
			line := fmt.Sprintf("%s %s  %-20s %-18s %s", cursor, shortID(pr.ID), state, pr.SourceBranch, pr.Title)
			if i == m.listCursor {
				line = cursorStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	help := "Keys: j/k move  enter open  q quit"
	if len(m.allRecords) != len(m.openRecords) || m.showAll {
		if m.showAll {
			help += "  a open"
		} else {
			help += "  a all"
		}
	}
	lines = append(lines, mutedStyle.Render(help))
	if m.skipped > 0 {
		lines = append(lines, mutedStyle.Render(app.SkippedRecordsMessage(m.skipped)))
	}
	if m.errMessage != "" {
		lines = append(lines, errorStyle.Render(m.errMessage))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderDetail() string {
	pr := m.currentPR
	if pr == nil {
		return ""
	}
	lines := []string{
		titleStyle.Render(fmt.Sprintf("PR %s: %s", shortID(pr.ID), pr.Title)),
		fmt.Sprintf("Source: %s  Base: %s  State: %s", pr.SourceBranch, pr.BaseBranch, pr.State),
	}
	if len(pr.Events) > 0 {
		latest := pr.Events[len(pr.Events)-1]
		lines = append(lines, fmt.Sprintf("Latest event: %s  Verdict: %s", latest.ID, latest.Verdict), fmt.Sprintf("Reviewed pair: %s / %s", latest.SourceHeadSHA, latest.BaseHeadSHA))
	} else {
		lines = append(lines, "Latest event: none")
	}
	open, resolved, outdated := 0, 0, 0
	for _, thread := range pr.Threads {
		if thread.Status == model.ThreadResolved {
			resolved++
		} else {
			open++
		}
		if thread.Outdated {
			outdated++
		}
	}
	lines = append(lines, fmt.Sprintf("Threads: %d open, %d resolved, %d outdated", open, resolved, outdated))
	if pr.Closure != nil {
		lines = append(lines, "Closure reason: "+string(pr.Closure.Reason))
		if pr.Closure.DestinationBranch != "" {
			lines = append(lines, "Destination: "+pr.Closure.DestinationBranch)
		}
		if len(pr.Closure.ResultingCommitSHAs) > 0 {
			lines = append(lines, "Resulting commits: "+strings.Join(pr.Closure.ResultingCommitSHAs, ", "))
		}
		if len(pr.Closure.PatchEquivalentIdentities) > 0 {
			lines = append(lines, "Patch identities: "+strings.Join(pr.Closure.PatchEquivalentIdentities, ", "))
		}
		if pr.Closure.ReplacingPRID != "" {
			lines = append(lines, "Superseded by: "+pr.Closure.ReplacingPRID)
		}
		if pr.Closure.Note != "" {
			lines = append(lines, "Closure note: "+pr.Closure.Note)
		}
	}
	lines = append(lines, "", mutedStyle.Render("Read-only PR view. Keys: d diff  c/r/m show CLI guidance  esc back  q quit"))
	if m.infoMessage != "" {
		lines = append(lines, infoStyle.Render(m.infoMessage))
	}
	if m.errMessage != "" {
		lines = append(lines, errorStyle.Render(m.errMessage))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) loadPRsCmd() tea.Cmd {
	return func() tea.Msg {
		records, skipped, err := m.svc.ListPRs("all")
		if err != nil {
			return listLoadedMsg{err: err}
		}
		return listLoadedMsg{records: records, skipped: skipped}
	}
}

func (m *Model) applyListMode() {
	if m.showAll {
		m.openRecords = append([]model.PR2(nil), m.allRecords...)
	} else {
		m.openRecords = m.openRecords[:0]
		for _, record := range m.allRecords {
			if record.State == model.PRStateOpen {
				m.openRecords = append(m.openRecords, record)
			}
		}
	}
	if len(m.openRecords) == 0 {
		m.listCursor = 0
	} else if m.listCursor >= len(m.openRecords) {
		m.listCursor = len(m.openRecords) - 1
	}
}

func (m *Model) loadPRCmd(id string) tea.Cmd {
	return func() tea.Msg {
		record, _, err := m.svc.LoadRecord(id)
		if err != nil {
			return prLoadedMsg{err: err}
		}
		return prLoadedMsg{record: record}
	}
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
