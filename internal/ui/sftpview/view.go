package sftpview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4")).
				Padding(0, 1)

	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#666")).
				Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)
)

// panelListInnerSize is the width/height passed to bubbles list.SetSize so each row fits
// inside activeBorderStyle (RoundedBorder + Padding(0,1)) without wrapping; outer is the
// lipgloss block size used in View (Width(panelWidth) / list viewport height budget).
func panelListInnerWidth(outer int) int {
	if outer <= 0 {
		return 0
	}
	n := outer - activeBorderStyle.GetHorizontalFrameSize()
	if n < 1 {
		return 1
	}
	return n
}

func panelListInnerHeight(outer int) int {
	if outer <= 0 {
		return 0
	}
	n := outer - activeBorderStyle.GetVerticalFrameSize()
	if n < 1 {
		return 1
	}
	return n
}

// composeFooter always returns exactly one line: transfer progress when active,
// otherwise a file-count summary. This guarantees SetSize reserves a fixed row
// so the progress bar never gets clipped.
func (m Model) composeFooter() string {
	if m.confirmMsg != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#e0a000")).Bold(true).
			Render(m.confirmMsg + "  [y/n]")
	}
	if m.transferring {
		pct := 0.0
		if m.progress.TotalBytes > 0 {
			pct = float64(m.progress.TransferredBytes) / float64(m.progress.TotalBytes) * 100
		}
		// [2/5] filename [====    ] 45.2%
		prefix := ""
		if m.progress.TotalFiles > 0 {
			prefix = fmt.Sprintf("[%d/%d] ", m.progress.FileIndex, m.progress.TotalFiles)
		}
		label := prefix + m.progress.CurrentFile + " "
		barWidth := m.width - lipgloss.Width(label) - 10 // 10 = [] + space + "100.0%"
		if barWidth < 8 {
			barWidth = 8
		}
		filled := int(pct / 100 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled
		bar := "[" + strings.Repeat("=", filled) + strings.Repeat(" ", empty) + "]"
		return progressStyle.Render(fmt.Sprintf("%s%s %.1f%%", label, bar, pct))
	}
	if m.err != "" {
		return errStyle.Render("Error: " + m.err)
	}
	// Idle: show file counts so the row is never empty.
	localN := len(m.localList.Items())
	remoteN := len(m.remoteList.Items())
	return helpStyle.Render(fmt.Sprintf("Local: %d items  |  Remote: %d items", localN, remoteN))
}

func (m Model) composeHelpLine() string {
	return helpStyle.Render("h/l:panel | enter:open | bksp:back | u:upload | d:download | x:delete | m:mkdir | r:rename | p:chmod")
}

func (m Model) View() tea.View {
	panelWidth := m.width/2 - 2
	if panelWidth < 0 {
		panelWidth = 0
	}

	var leftStyle, rightStyle lipgloss.Style
	if m.focusedPanel == leftPanel {
		leftStyle = activeBorderStyle.Width(panelWidth)
		rightStyle = inactiveBorderStyle.Width(panelWidth)
	} else {
		leftStyle = inactiveBorderStyle.Width(panelWidth)
		rightStyle = activeBorderStyle.Width(panelWidth)
	}

	leftPanel := leftStyle.Render(m.localList.View())
	rightPanel := rightStyle.Render(m.remoteList.View())

	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Page indicators for both panels
	pageInfo := m.composePaginationLine(panelWidth)

	footer := m.composeFooter()
	helpLine := m.composeHelpLine()

	var parts []string
	parts = append(parts, panels)
	parts = append(parts, pageInfo)
	parts = append(parts, footer)
	parts = append(parts, helpLine)
	main := strings.Join(parts, "\n")
	if m.chmodActive {
		main = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.renderChmodOverlay())
	}
	return tea.NewView(main)
}

var (
	pageNumStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	pageIndicatorDim = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
)

func (m Model) composePaginationLine(panelWidth int) string {
	render := func(p paginator.Model) string {
		page := p.Page + 1
		total := p.TotalPages
		if total < 1 {
			total = 1
		}
		var parts []string
		if page > 1 {
			parts = append(parts, pageIndicatorDim.Render("◀ "))
		} else {
			parts = append(parts, "  ")
		}
		parts = append(parts, pageNumStyle.Render(fmt.Sprintf("%d", page)))
		parts = append(parts, pageIndicatorDim.Render(" / "))
		parts = append(parts, pageIndicatorDim.Render(fmt.Sprintf("%d", total)))
		if page < total {
			parts = append(parts, pageIndicatorDim.Render(" ▶"))
		}
		return strings.Join(parts, "")
	}
	leftPage := render(m.localList.Paginator)
	rightPage := render(m.remoteList.Paginator)
	// Pad each to panelWidth + border (2) to align with panels
	pw := panelWidth + 2
	leftPad := lipgloss.NewStyle().Width(pw).Align(lipgloss.Center).Render(leftPage)
	rightPad := lipgloss.NewStyle().Width(pw).Align(lipgloss.Center).Render(rightPage)
	return leftPad + rightPad
}

func (m Model) renderChmodOverlay() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Change Permissions")
	path := helpStyle.Render(m.chmodPath)
	hint := helpStyle.Render("Enter apply | Esc cancel | click left apply / right cancel | octal only")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", path, "", m.chmodInput.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}
