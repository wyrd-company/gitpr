package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wyrd-company/gitpr/internal/model"
)

type pickerModel struct {
	title    string
	prs      []model.PR
	cursor   int
	width    int
	height   int
	selected string
	canceled bool
}

func SelectPR(title string, prs []model.PR) (string, error) {
	if len(prs) == 0 {
		return "", nil
	}

	m := pickerModel{
		title:  title,
		prs:    prs,
		cursor: 0,
	}

	program := tea.NewProgram(m, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return "", err
	}

	finalModel, ok := result.(pickerModel)
	if !ok || finalModel.canceled {
		return "", nil
	}

	return finalModel.selected, nil
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.prs)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.prs) > 0 {
				m.selected = m.prs[m.cursor].ID
			}
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m pickerModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var lines []string
	lines = append(lines, titleStyle.Render(m.title))
	lines = append(lines, "")

	for i, pr := range m.prs {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}

		line := fmt.Sprintf("%s %s  %-10s %-18s %s", cursor, shortID(pr.ID), pr.Status, pr.SourceBranch, pr.Title)
		if i == m.cursor {
			line = cursorStyle.Render(line)
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("Keys: j/k move  enter select  esc cancel"))

	return strings.Join(lines, "\n")
}
