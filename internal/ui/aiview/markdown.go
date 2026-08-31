package aiview

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
)

type markdown struct {
	full      *glamour.TermRenderer
	transient *glamour.TermRenderer
	width     int
}

func newMarkdown(width int) *markdown {
	if width < 8 {
		width = 8
	}
	full, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.DarkStyle),
		glamour.WithWordWrap(width),
	)
	cfg := styles.DarkStyleConfig
	cfg.CodeBlock.Theme = ""
	cfg.CodeBlock.Chroma = nil
	transient, _ := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(width),
	)
	return &markdown{full: full, transient: transient, width: width}
}

func (m *markdown) render(text string, final bool) string {
	r := m.transient
	if final {
		r = m.full
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return out
}
