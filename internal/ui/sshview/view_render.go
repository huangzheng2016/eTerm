package sshview

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
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
	switch {
	case m.sel.active && !m.emu.IsAltScreen():
		screen = m.renderWithSelection()
	case m.scrollOffset > 0 && !m.emu.IsAltScreen():
		screen = m.renderScrollback()
	case m.bottomPad > 0 && !m.emu.IsAltScreen():
		screen = m.renderBottomPad()
	default:
		screen = m.renderScreenWithCursor()
	}
	if m.disconnected {
		key := viewkeys.HelpLabel(m.vk.Reconnect)
		if key == "" {
			key = "r"
		}
		reason := "Connection lost"
		if ce := internalssh.Classify(m.endErr); ce != nil {
			reason = ce.Summary
		}
		banner := disconnectBannerStyle.Render(reason + " · press " + key + " to reconnect")
		screen = strings.TrimRight(screen, "\n") + "\n\n" + banner
	}
	v := tea.NewView(screen)
	return v
}

// renderWithSelection renders the visible body cell-by-cell, highlighting cells that
// fall inside the active selection. Used only while selecting; the normal path keeps
// the fast emu.Render().
func (m *Model) renderWithSelection() string {
	w, h := m.emu.Width(), m.emu.Height()
	if w <= 0 || h <= 0 {
		return m.emu.Render()
	}
	start, end := normSel(m.sel)
	var lines []string
	for y := 0; y < h; y++ {
		absLine := m.visibleAbsLine(y)
		var line strings.Builder
		for col := 0; col < w; {
			cell := m.cellAtAbs(absLine, col)
			width := 1
			if cell != nil && cell.Width > 1 {
				width = cell.Width
			}
			content := " "
			if cell != nil && cell.Content != "" {
				content = cell.Content
			}
			if inSelection(start, end, absLine, col) {
				st := uv.Style{}
				if cell != nil {
					st = cell.Style
				}
				sel := selectionCellStyle(st)
				line.WriteString((&sel).Styled(content))
			} else if cell != nil && cell.Content != "" {
				line.WriteString(renderCellANSI(cell))
			} else {
				line.WriteString(content)
			}
			if width < 1 {
				width = 1
			}
			col += width
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func selectionCellStyle(s uv.Style) uv.Style {
	out := s
	out.Bg = color.RGBA{R: 0x3a, G: 0x3d, B: 0x6e, A: 0xff}
	if out.Fg == nil {
		out.Fg = color.RGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}
	}
	return out
}

// renderBottomPad shows the live screen pushed up by bottomPad rows, with empty
// rows below, so the user can scroll past the bottom to see the newest line clearly.
func (m *Model) renderBottomPad() string {
	lines := strings.Split(m.renderScreenWithCursor(), "\n")
	if m.bottomPad < len(lines) {
		lines = lines[m.bottomPad:]
	}
	for i := 0; i < m.bottomPad; i++ {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
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

	if len(lines) > 0 && m.scrollIndicatorVisible(time.Now()) {
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("[%d/%d]", offset, sbLen))
		lines[0] = overlayRight(lines[0], w, indicator)
	}

	return strings.Join(lines, "\n")
}

func overlayRight(line string, width int, overlay string) string {
	overlayW := lipgloss.Width(overlay)
	if width <= 0 || overlayW >= width {
		return ansi.Cut(overlay, 0, width)
	}
	baseW := width - overlayW
	base := ansi.Cut(line, 0, baseW)
	if pad := baseW - lipgloss.Width(base); pad > 0 {
		base += strings.Repeat(" ", pad)
	}
	return base + overlay
}

func renderScrollbackLine(m *Model, w, idx int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		cell := m.emu.ScrollbackCellAt(x, idx)
		if cell != nil && cell.Width == 0 {
			continue
		}
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
		if cell != nil && cell.Width == 0 {
			continue
		}
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
