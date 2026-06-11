package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var EmptyHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

type Page struct {
	Width      int
	Height     int
	Header     string
	Body       string
	Footer     string
	CenterBody bool
}

func (p Page) Render() string {
	header := strings.TrimRight(p.Header, "\n")
	body := strings.TrimRight(p.Body, "\n")
	footer := strings.TrimRight(p.Footer, "\n")
	if p.Height <= 0 {
		return joinNonEmpty(header, body, footer)
	}

	headerH := blockHeight(header)
	footerH := blockHeight(footer)
	bodyH := p.Height - headerH - footerH
	if bodyH < 0 {
		bodyH = 0
	}
	if p.CenterBody {
		body = Center(p.Width, bodyH, body)
	} else {
		body = fitHeight(body, bodyH)
	}
	return joinNonEmpty(header, body, footer)
}

func EmptyState(width, height int, lines ...string) string {
	return Center(width, height, emptyStateBlock(width, lines...))
}

func Loading(width, height int, text string) string {
	return EmptyState(width, height, text)
}

func Center(width, height int, content string) string {
	content = strings.TrimRight(content, "\n")
	if width <= 0 || height <= 0 {
		return content
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func emptyStateBlock(width int, lines ...string) string {
	rows := make([]string, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		line = truncateLine(line, width)
		if i == 0 {
			rows = append(rows, line)
		} else {
			rows = append(rows, EmptyHintStyle.Render(line))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

func truncateLine(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return "…"
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n")
}

func blockHeight(s string) int {
	if s == "" {
		return 0
	}
	return lipgloss.Height(s)
}

func fitHeight(s string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
