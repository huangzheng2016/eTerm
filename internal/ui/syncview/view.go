package syncview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	formStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7D56F4")).Padding(1, 3).Width(60)
	labelStyle  = lipgloss.NewStyle().Width(16).Foreground(lipgloss.Color("#7D56F4"))
	focusLabel  = lipgloss.NewStyle().Width(16).Foreground(lipgloss.Color("#FF79C6")).Bold(true)
	arrowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	arrowFocus  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF79C6"))
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
	selFocus    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF79C6")).Bold(true)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
)

func (m *Model) View() tea.View {
	var rows []string

	title := headerStyle.Render("Sync Settings")
	rows = append(rows, title, "")

	vf := m.visibleFields()
	for i, f := range vf {
		focused := i == m.focused
		lbl := labelStyle
		if focused {
			lbl = focusLabel
		}

		var value string
		switch f {
		case fieldEnabled:
			value = m.renderSelector(enableOptions[m.enableIdx], focused)
		case fieldMode:
			value = m.renderSelector(modeOptions[m.modeIdx], focused)
		case fieldInsecureTLS:
			value = m.renderSelector(insecureOptions[m.insecureIdx], focused)
		case fieldSSHHost:
			name := "(none)"
			if m.hostIdx >= 0 && m.hostIdx < len(m.hostOpts) {
				h := m.hostOpts[m.hostIdx]
				name = fmt.Sprintf("%s (%s@%s:%d)", h.Alias, h.Username, h.Hostname, h.Port)
			}
			value = m.renderSelector(name, focused)
		default:
			idx := m.inputIdxForField(f)
			if idx >= 0 {
				value = m.inputs[idx].View()
			}
		}

		label := m.fieldLabel(f)
		rows = append(rows, fmt.Sprintf("  %s  %s", lbl.Render(label), value))
	}

	rows = append(rows, "")
	if m.err != "" {
		rows = append(rows, "  "+errStyle.Render(m.err))
	}
	rows = append(rows, "  "+hintStyle.Render("Tab:next | Left/Right:select | C-s:save | F5:test | C-y:sync | Esc:close"))

	content := strings.Join(rows, "\n")
	box := formStyle.Render(content)
	return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box))
}

func (m *Model) renderSelector(text string, focused bool) string {
	as := arrowStyle
	ss := selStyle
	if focused {
		as = arrowFocus
		ss = selFocus
	}
	return fmt.Sprintf("%s %s %s", as.Render("<"), ss.Render(text), as.Render(">"))
}

func (m *Model) fieldLabel(f int) string {
	switch f {
	case fieldEnabled:
		return "Enabled"
	case fieldMode:
		return "Mode"
	case fieldSSHHost:
		return "SSH Host"
	case fieldRemotePort:
		return "Remote Port"
	case fieldServerURL:
		return "Server URL"
	case fieldInsecureTLS:
		return "Insecure TLS"
	case fieldAPIKey:
		return "API Key"
	case fieldPassphrase:
		return "Passphrase"
	case fieldInterval:
		return "Interval (sec)"
	}
	return ""
}
