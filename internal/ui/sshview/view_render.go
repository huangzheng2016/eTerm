package sshview

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// vt.Render() draws the cell grid but does not include a visible text cursor (the TUI
// cursor is tracked internally only). We briefly invert the cursor cell so the block
// is visible like a real terminal.
var disconnectBannerStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#e0a000")).
	Bold(true)

var scrollIndicatorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#7D56F4")).
	Bold(true)

func (m *Model) View() tea.View {
	var screen string
	if m.scrollOffset > 0 && !m.emu.IsAltScreen() {
		screen = m.renderScrollback()
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf(" ↑ scrollback [%d lines up] — type to return ", m.scrollOffset))
		screen = strings.TrimRight(screen, "\n") + "\n" + indicator
	} else {
		screen = m.renderScreenWithCursor()
	}
	if m.disconnected {
		banner := disconnectBannerStyle.Render("Connection lost · press r to reconnect")
		screen = strings.TrimRight(screen, "\n") + "\n\n" + banner
	}
	v := tea.NewView(screen)
	return v
}

func (m *Model) renderScreenWithCursor() string {
	w, h := m.emu.Width(), m.emu.Height()
	pos := m.emu.CursorPosition()
	cx, cy := pos.X, pos.Y
	if w <= 0 || h <= 0 || cx < 0 || cy < 0 || cx >= w || cy >= h {
		return m.emu.Render()
	}
	orig := m.emu.CellAt(cx, cy)
	if orig == nil {
		return m.emu.Render()
	}

	saved := *orig
	highlight := saved
	highlight.Style = invertCursorStyle(saved.Style)
	if highlight.Content == "" {
		highlight.Content = " "
		highlight.Width = 1
	}

	m.emu.SetCell(cx, cy, &highlight)
	out := m.emu.Render()
	m.emu.SetCell(cx, cy, &saved)
	return out
}

// renderScrollback renders a mixed view: scrollback lines at the top, then
// visible screen lines at the bottom, offset by m.scrollOffset.
func (m *Model) renderScrollback() string {
	w := m.emu.Width()
	h := m.emu.Height()
	sbLen := m.emu.ScrollbackLen()
	if sbLen == 0 || h <= 0 || w <= 0 {
		return m.emu.Render()
	}

	// We want to show h lines total.
	// scrollOffset=1 means the top line is the last scrollback line,
	// and the bottom h-1 lines are from the current screen.
	offset := m.scrollOffset
	if offset > sbLen {
		offset = sbLen
	}

	var lines []string
	// How many lines come from scrollback vs current screen
	sbLines := offset // lines from scrollback
	screenLines := h - sbLines
	if screenLines < 0 {
		screenLines = 0
		sbLines = h
	}

	// Render scrollback lines (oldest first in scrollback, we want newest first)
	sbStart := sbLen - offset
	if sbStart < 0 {
		sbStart = 0
	}
	for i := 0; i < sbLines && i < h; i++ {
		idx := sbStart + i
		if idx >= sbLen {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, renderScrollbackLine(m, w, idx))
	}

	// Render current screen lines (top screenLines lines of the screen)
	for y := 0; y < screenLines && y < h; y++ {
		lines = append(lines, renderScreenLine(m, w, y))
	}

	return strings.Join(lines, "\n")
}

func renderScrollbackLine(m *Model, w, idx int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		cell := m.emu.ScrollbackCellAt(x, idx)
		if cell == nil || cell.Content == "" {
			sb.WriteByte(' ')
		} else {
			sb.WriteString(renderCellANSI(cell))
		}
	}
	return sb.String()
}

func renderScreenLine(m *Model, w, y int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		cell := m.emu.CellAt(x, y)
		if cell == nil || cell.Content == "" {
			sb.WriteByte(' ')
		} else {
			sb.WriteString(renderCellANSI(cell))
		}
	}
	return sb.String()
}

func renderCellANSI(cell *uv.Cell) string {
	content := cell.Content
	if content == "" {
		content = " "
	}
	// Use ultraviolet's built-in ANSI styling
	return cell.Style.Styled(content)
}

func invertCursorStyle(s uv.Style) uv.Style {
	out := s
	out.Fg, out.Bg = s.Bg, s.Fg

	// Default colors: typical dark terminal — light glyph on gray block.
	if s.Fg == nil && s.Bg == nil {
		out.Fg = color.RGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}
		out.Bg = color.RGBA{R: 0x44, G: 0x44, B: 0x44, A: 0xff}
		return out
	}
	if out.Fg == nil {
		out.Fg = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	if out.Bg == nil {
		out.Bg = color.RGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff}
	}
	return out
}
