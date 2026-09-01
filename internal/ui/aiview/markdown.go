package aiview

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

type markdown struct {
	full      *glamour.TermRenderer
	transient *glamour.TermRenderer
	// wide renders the same text without wrapping; alignBreaks compares the
	// wrapped output against it to find soft-wrap continuations for copying.
	fullWide      *glamour.TermRenderer
	transientWide *glamour.TermRenderer
	width         int
}

func newMarkdown(width int) *markdown {
	if width < 8 {
		width = 8
	}
	full, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.DarkStyle),
		glamour.WithWordWrap(width),
	)
	fullWide, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.DarkStyle),
		glamour.WithWordWrap(10000),
	)
	cfg := styles.DarkStyleConfig
	cfg.CodeBlock.Theme = ""
	cfg.CodeBlock.Chroma = nil
	transient, _ := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(width),
	)
	transientWide, _ := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(10000),
	)
	return &markdown{full: full, transient: transient, fullWide: fullWide, transientWide: transientWide, width: width}
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

// renderLogical renders text unwrapped and returns its plain-text lines:
// the logical lines the wrapped display lines were broken from.
func (m *markdown) renderLogical(text string, final bool) []string {
	r := m.transientWide
	if final {
		r = m.fullWide
	}
	out, err := r.Render(text)
	if err != nil {
		return strings.Split(text, "\n")
	}
	return strings.Split(strings.Trim(ansi.Strip(out), "\n"), "\n")
}
