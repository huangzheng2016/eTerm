package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type StatusBar struct {
	text   string
	width  int
	locked bool
}

func NewStatusBar() StatusBar {
	return StatusBar{}
}

func (s StatusBar) SetWidth(w int) StatusBar {
	s.width = w
	return s
}

func (s StatusBar) SetText(t string) StatusBar {
	s.text = t
	return s
}

func (s StatusBar) SetLocked(l bool) StatusBar {
	s.locked = l
	return s
}

func (s StatusBar) View() string {
	left := s.text
	if s.locked {
		left = "🔒 " + left
	}

	// Single-line bar: truncate if the shortcut line exceeds width.
	const right = ""
	maxLeft := s.width
	if right != "" {
		maxLeft -= ansi.StringWidth(right)
	}
	if maxLeft < 1 {
		maxLeft = 1
	}
	if ansi.StringWidth(left) > maxLeft {
		left = ansi.Truncate(left, maxLeft, "…")
	}

	gap := s.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	bar := left + strings.Repeat(" ", gap) + right
	return ui.StatusBarStyle.Width(s.width).MaxHeight(1).Render(bar)
}
