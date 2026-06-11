package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type connErrorModel struct {
	ce       *internalssh.ConnectError
	target   string
	retry    tea.Msg
	expanded bool
}

func newConnErrorModel(ce *internalssh.ConnectError, target string, retry tea.Msg) *connErrorModel {
	return &connErrorModel{ce: ce, target: target, retry: retry}
}

func (m *connErrorModel) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Bold(true)
	lines := []string{titleStyle.Render("Connection error: " + m.ce.Summary)}
	if m.target != "" {
		lines = append(lines, ui.DimStyle.Render("Host: "+m.target))
	}
	if m.ce.Hint != "" {
		lines = append(lines, "", lipgloss.NewStyle().Width(64).Render(m.ce.Hint))
	}
	if m.expanded {
		detail := strings.TrimRight(m.ce.Err.Error(), "\n")
		lines = append(lines, "", ui.DimStyle.Render("Details:"),
			lipgloss.NewStyle().Width(64).Foreground(lipgloss.Color("#bbb")).Render(detail))
	}

	toggle := "show"
	if m.expanded {
		toggle = "hide"
	}
	parts := []string{"d: " + toggle + " details"}
	if m.retry != nil {
		parts = append(parts, "r: retry")
	}
	parts = append(parts, "esc: close")
	lines = append(lines, "", ui.DimStyle.Render(strings.Join(parts, "   ")))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#d32f2f")).
		Padding(1, 2).
		Render(content)
}

func (a App) handleConnErrorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		a.connError = nil
		return a, nil
	case "d", "D":
		a.connError.expanded = !a.connError.expanded
		return a, nil
	case "r", "R":
		retry := a.connError.retry
		a.connError = nil
		if retry == nil {
			return a, nil
		}
		return a, func() tea.Msg { return retry }
	}
	return a, nil
}

// connErrorMouse toggles the detail panel when the card is clicked.
func (a App) connErrorMouse(lx, ly int) (tea.Model, tea.Cmd) {
	if a.connError == nil {
		return a, nil
	}
	a.connError.expanded = !a.connError.expanded
	return a, nil
}
