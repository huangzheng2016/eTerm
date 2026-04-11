package editor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	labelStyle = lipgloss.NewStyle().
			Width(14).
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	focusedLabelStyle = lipgloss.NewStyle().
				Width(14).
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	selectorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	selectorFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	arrowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999"))

	arrowFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true).
			MarginTop(1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999")).
			MarginTop(1)

	formStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3).
			Width(60)
)

func (m Model) View() tea.View {
	var title string
	if m.host != nil && m.host.ID > 0 {
		title = titleStyle.Render("Edit Host")
	} else {
		title = titleStyle.Render("New Host")
	}

	vf := m.visibleFields()
	rows := make([]string, 0, len(vf)+4)
	rows = append(rows, title)
	rows = append(rows, "")

	for vi, field := range vf {
		focused := vi == m.focused
		lbl := fieldLabels[field]
		ls := labelStyle
		if focused {
			ls = focusedLabelStyle
		}
		label := ls.Render(lbl)

		var value string
		switch field {
		case authMethodField:
			value = m.renderSelector(authOptions[m.authIdx], focused)
		case proxyTypeField:
			value = m.renderSelector(proxyDisplay[m.proxyTypeIdx], focused)
		case gssapiSourceField:
			value = m.renderSelector(gssapiSourceDisplay[m.gssapiSourceIdx], focused)
		case keyIDField:
			name := "(none)"
			hint := ""
			if m.keyIdx >= 0 && m.keyIdx < len(m.keyOptions) {
				k := m.keyOptions[m.keyIdx]
				name = k.Name
				if len(name) > 28 {
					name = name[:27] + "…"
				}
				fp := k.Fingerprint
				if len(fp) > 44 {
					fp = fp[:43] + "…"
				}
				hint = k.Type + " " + fp
			}
			value = m.renderSelector(name, focused)
			if hint != "" {
				rows = append(rows, fmt.Sprintf("%s %s", label, value))
				hintStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(hint)
				rows = append(rows, hintStyled)
				continue
			}
		case jumpHostField:
			name := "(none)"
			if m.jumpIdx >= 0 && m.jumpIdx < len(m.jumpHostOptions) {
				jh := m.jumpHostOptions[m.jumpIdx]
				label := strings.TrimSpace(jh.Alias)
				if label == "" {
					label = jh.Hostname
				}
				name = label
			}
			value = m.renderSelector(name, focused)
		default:
			idx := inputIndexForField(field)
			if idx >= 0 {
				value = m.inputs[idx].View()
			}
		}

		rows = append(rows, fmt.Sprintf("%s %s", label, value))
	}

	if m.err != "" {
		rows = append(rows, errorStyle.Render(fmt.Sprintf("\u26a0 %s", m.err)))
	}

	rows = append(rows, footerStyle.Render("Tab/\u2193:next | Shift+Tab/\u2191:prev | \u2190\u2192:select | Ctrl+S:save | Esc:cancel"))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := formStyle.Render(content)

	if m.width > 0 && m.height > 0 {
		box = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	return tea.NewView(box)
}

func (m Model) renderSelector(text string, focused bool) string {
	as := arrowStyle
	ss := selectorStyle
	if focused {
		as = arrowFocusedStyle
		ss = selectorFocusedStyle
	}
	return fmt.Sprintf("%s %s %s", as.Render("\u25c0"), ss.Render(text), as.Render("\u25b6"))
}
