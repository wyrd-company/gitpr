package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/wyrd-company/gitpr/internal/app"
	"github.com/wyrd-company/gitpr/internal/model"
)

type screen int

const (
	listScreen screen = iota
	detailScreen
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

	openRecords []model.PR2
	allRecords  []model.PR2
	skipped     int
	showAll     bool
	listCursor  int
	currentPR   *model.PR2
	infoMessage string
	errMessage  string
}

func Run(svc *app.Service) error {
	m := &Model{svc: svc, screen: listScreen}

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
		record := msg.record
		m.currentPR = &record
		m.screen = detailScreen
		m.infoMessage = ""
		m.errMessage = ""
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
		return m, tea.Quit
	case "esc":
		m.screen, m.currentPR, m.errMessage = listScreen, nil, ""
		return m, m.loadPRsCmd()
	case "c", "r", "m":
		m.infoMessage = "PR actions use the CLI: gitpr review <id>, approve/reject with its basis, gitpr merge <id>, or gitpr comment <id>."
	}
	return m, nil
}

func (m *Model) renderList() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

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
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.errMessage))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) renderDetail() string {
	pr := m.currentPR
	if pr == nil {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
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
	lines = append(lines, "", mutedStyle.Render("Read-only PR view. Keys: c/r/m show CLI guidance  esc back  q quit"))
	if m.infoMessage != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render(m.infoMessage))
	}
	if m.errMessage != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.errMessage))
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
