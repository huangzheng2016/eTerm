package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/version"
)

const upgradePromptItemRow = 4

func NewUpgradePrompt(tag, htmlURL string) *upgradePromptModel {
	archive, inner, ok := version.ReleaseArchiveNames()
	return &upgradePromptModel{
		Tag:           tag,
		ReleaseURL:    htmlURL,
		Archive:       archive,
		Inner:         inner,
		SupportedArch: ok,
		Cursor:        0,
	}
}

type upgradePromptModel struct {
	Tag           string
	ReleaseURL    string
	Archive       string
	Inner         string
	SupportedArch bool

	Busy     bool
	BusyHint string

	Cursor int
}

func (m *upgradePromptModel) menuLen() int {
	if m.SupportedArch {
		return 4
	}
	return 2
}

func (m *upgradePromptModel) clampCursor() {
	max := m.menuLen() - 1
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor > max {
		m.Cursor = max
	}
}

func upgradePromptRowLabel(m *upgradePromptModel, logicalIndex int) string {
	if !m.SupportedArch {
		switch logicalIndex {
		case 0:
			return " Open release page   "
		case 1:
			return " Later               "
		}
		return ""
	}
	switch logicalIndex {
	case 0:
		return " Install and exit    "
	case 1:
		return " Download only       "
	case 2:
		return " Open release page   "
	case 3:
		return " Later               "
	}
	return ""
}

func upgradePromptKeyHint(m *upgradePromptModel, logicalIndex int) string {
	if !m.SupportedArch {
		switch logicalIndex {
		case 0:
			return "o"
		case 1:
			return "esc"
		}
	}
	switch logicalIndex {
	case 0:
		return "i"
	case 1:
		return "d"
	case 2:
		return "o"
	case 3:
		return "esc"
	}
	return ""
}

func upgradePromptView(m *upgradePromptModel) string {
	if m.Busy {
		title := ui.TitleStyle.Render("Update")
		body := lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc")).Render(m.BusyHint)
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", body)
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3).
			Width(46).
			Render(content)
	}

	title := ui.TitleStyle.Render("Update available")
	tagLine := ui.DimStyle.Render(fmt.Sprintf("Release %s", m.Tag))
	var platLine string
	if m.SupportedArch {
		platLine = ui.DimStyle.Render(fmt.Sprintf("Package: %s", m.Archive))
	} else {
		platLine = ui.DimStyle.Render("No prebuilt package for this platform.")
	}

	n := m.menuLen()
	var b strings.Builder
	for i := 0; i < n; i++ {
		style := ui.DimStyle
		cursorMark := "  "
		if i == m.Cursor {
			cursorMark = "▸ "
			style = ui.SelectedStyle
		}
		row := fmt.Sprintf("%s%s  %s",
			cursorMark,
			style.Render(upgradePromptRowLabel(m, i)),
			ui.DimStyle.Render("["+upgradePromptKeyHint(m, i)+"]"),
		)
		b.WriteString(row)
		b.WriteByte('\n')
	}

	h := ui.DimStyle.Render("↑↓ · enter · i/d/o · esc Later")
	content := lipgloss.JoinVertical(lipgloss.Left,
		title, tagLine, platLine,
		"",
		strings.TrimSuffix(b.String(), "\n"),
		"",
		h,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(52).
		Render(content)
}
