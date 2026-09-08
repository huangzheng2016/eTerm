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

var disconnectBannerStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#e0a000")).
	Bold(true)

var disconnectedBadgeStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#ffffff")).
	Background(lipgloss.Color("#d20f39")).
	Bold(true).
	Padding(0, 1)

var reconnectingBadgeStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#111111")).
	Background(lipgloss.Color("#f2cc60")).
	Bold(true).
	Padding(0, 1)

var scrollIndicatorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#7D56F4")).
	Bold(true)

func (m *Model) View() tea.View {
	cur, reportCur := m.reportCursor()
	var screen string
	switch {
	case m.sel.active && !m.emu.IsAltScreen():
		screen = m.renderWithSelection()
	case m.scrollOffset > 0 && !m.emu.IsAltScreen():
		screen = m.renderScrollback()
	case m.bottomPad > 0 && !m.emu.IsAltScreen():
		screen = m.renderBottomPad()
	case reportCur:
		screen = m.renderScreen()
	default:
		screen = m.renderScreenWithCursor()
	}
	if m.disconnected {
		if m.reconnecting {
			banner := disconnectBannerStyle.Render("Reconnecting...")
			screen = strings.TrimRight(screen, "\n") + "\n\n" + banner
			v := tea.NewView(screen)
			v.Cursor = cur
			return v
		}
		screen = overlayFirstLineRight(screen, m.emu.Width(), disconnectedBadgeStyle.Render("DISCONNECTED"))
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
	v.Cursor = cur
	return v
}

func (m *Model) reportCursor() (*tea.Cursor, bool) {
	if m.sel.active || m.scrollOffset > 0 || m.bottomPad > 0 {
		return nil, false
	}
	w, h := m.emu.Width(), m.emu.Height()
	pos := m.emu.CursorPosition()
	if w <= 0 || h <= 0 || pos.X < 0 || pos.Y < 0 || pos.X >= w || pos.Y >= h {
		return nil, false
	}
	return tea.NewCursor(pos.X, pos.Y), true
}

func (m *Model) renderScreen() string {
	if m.emu.IsAltScreen() {
		return m.renderFullScreen()
	}
	return m.emu.Render()
}

func overlayFirstLineRight(screen string, width int, overlay string) string {
	lines := strings.Split(screen, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	lines[0] = overlayRight(lines[0], width, overlay)
	return strings.Join(lines, "\n")
}

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
		var link uv.Link
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
				var lk uv.Link
				if cell != nil {
					st = cell.Style
					lk = cell.Link
				}
				sel := selectionCellStyle(st)
				writeLink(&line, &link, lk)
				line.WriteString((&sel).Styled(content))
			} else if cell != nil && cell.Content != "" {
				writeLink(&line, &link, cell.Link)
				line.WriteString(renderCellANSI(cell))
			} else {
				writeLink(&line, &link, uv.Link{})
				line.WriteString(content)
			}
			if width < 1 {
				width = 1
			}
			col += width
		}
		writeLink(&line, &link, uv.Link{})
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
	if m.cursorHidden || w <= 0 || h <= 0 || cx < 0 || cy < 0 || cx >= w || cy >= h {
		if m.emu.IsAltScreen() {
			return m.renderFullScreen()
		}
		return m.emu.Render()
	}
	orig := m.emu.CellAt(cx, cy)
	if orig == nil {
		if m.emu.IsAltScreen() {
			return m.renderFullScreen()
		}
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
	var out string
	if m.emu.IsAltScreen() {
		out = m.renderFullScreen()
	} else {
		out = m.emu.Render()
	}
	m.emu.SetCell(cx, cy, &saved)
	return out
}

func (m *Model) renderFullScreen() string {
	w, h := m.emu.Width(), m.emu.Height()
	if w <= 0 || h <= 0 {
		return m.emu.Render()
	}
	lines := make([]string, 0, h)
	for y := 0; y < h; y++ {
		lines = append(lines, renderScreenLine(m, w, y))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderScrollback() string {
	w := m.emu.Width()
	h := m.emu.Height()
	sbLen := m.emu.ScrollbackLen()
	if sbLen == 0 || h <= 0 || w <= 0 {
		return m.emu.Render()
	}

	offset := m.scrollOffset
	if offset > sbLen {
		offset = sbLen
	}

	var lines []string
	sbLines := offset
	screenLines := h - sbLines
	if screenLines < 0 {
		screenLines = 0
		sbLines = h
	}

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
	var link uv.Link
	for x := 0; x < w; x++ {
		cell := m.emu.ScrollbackCellAt(x, idx)
		if cell != nil && cell.Width == 0 {
			continue
		}
		if cell == nil || cell.Content == "" {
			writeLink(&sb, &link, uv.Link{})
			sb.WriteByte(' ')
		} else {
			writeLink(&sb, &link, cell.Link)
			sb.WriteString(renderCellANSI(cell))
		}
	}
	writeLink(&sb, &link, uv.Link{})
	return sb.String()
}

func renderScreenLine(m *Model, w, y int) string {
	var sb strings.Builder
	var pen uv.Style
	var link uv.Link
	for x := 0; x < w; x++ {
		cell := m.emu.CellAt(x, y)
		if cell != nil && cell.Width == 0 {
			continue
		}
		content := " "
		var st uv.Style
		var lk uv.Link
		if cell != nil && cell.Content != "" {
			content = cell.Content
			st = cell.Style
			lk = cell.Link
		}
		if !st.Equal(&pen) {
			sb.WriteString(st.Diff(&pen))
			pen = st
		}
		writeLink(&sb, &link, lk)
		sb.WriteString(content)
	}
	writeLink(&sb, &link, uv.Link{})
	if !pen.IsZero() {
		sb.WriteString(ansi.ResetStyle)
	}
	return sb.String()
}

func writeLink(sb *strings.Builder, cur *uv.Link, next uv.Link) {
	if next == *cur {
		return
	}
	if cur.URL != "" {
		sb.WriteString(ansi.ResetHyperlink())
	}
	if next.URL != "" {
		sb.WriteString(ansi.SetHyperlink(next.URL, next.Params))
	}
	*cur = next
}

func renderCellANSI(cell *uv.Cell) string {
	content := cell.Content
	if content == "" {
		content = " "
	}
	return cell.Style.Styled(content)
}

func invertCursorStyle(s uv.Style) uv.Style {
	out := s
	out.Fg, out.Bg = s.Bg, s.Fg

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
