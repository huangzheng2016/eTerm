package aiview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// copyLastReply (/copy) puts the newest assistant reply on the clipboard as
// plain text.
func (m *Model) copyLastReply() tea.Cmd {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == blockAssistant && m.blocks[i].text != "" {
			text := m.blocks[i].text
			return tea.Batch(
				tea.SetClipboard(text),
				m.setToast(fmt.Sprintf("Copied %d chars", len([]rune(text)))),
			)
		}
	}
	return m.slashError("no assistant reply to copy")
}

// exportSession (/export) writes the conversation as Markdown into
// ~/Downloads (falling back to the home directory) and toasts the path.
func (m *Model) exportSession() tea.Cmd {
	home, err := os.UserHomeDir()
	if err != nil {
		return m.slashError("export: " + err.Error())
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = home
	}
	path := filepath.Join(dir, "eterm-ai-"+time.Now().Format("20060102-150405")+".md")
	if err := os.WriteFile(path, []byte(m.exportMarkdown()), 0o644); err != nil {
		return m.slashError("export: " + err.Error())
	}
	shown := path
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		shown = "~/" + rel
	}
	return m.setToast("Exported to " + shown)
}

// exportMarkdown renders blocks in one plain format per kind.
func (m *Model) exportMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# eTerm AI session\n")
	for _, b := range m.blocks {
		sb.WriteString("\n")
		switch b.kind {
		case blockUser:
			sb.WriteString("## You\n\n" + b.text + "\n")
		case blockAssistant:
			sb.WriteString("## Assistant\n\n" + b.text + "\n")
		case blockThinking:
			sb.WriteString("### Thinking\n\n" + b.text + "\n")
		case blockTool:
			sb.WriteString("### Tool: " + b.text + "\n\n")
			if b.args != "" {
				sb.WriteString(b.args + "\n\n")
			}
			if out := strings.TrimRight(b.output, "\n"); out != "" {
				sb.WriteString("```\n" + out + "\n```\n")
			}
		case blockSystem:
			sb.WriteString("### System\n\n" + b.text + "\n")
		}
	}
	return sb.String()
}

type compactDoneMsg struct {
	stats CompactStats
	err   error
}

// compactSession (/compact) runs agent compaction off the UI goroutine; the
// result lands as a system block via compactDoneMsg.
func (m *Model) compactSession() tea.Cmd {
	return func() tea.Msg {
		stats, err := m.runner.Compact(context.Background())
		return compactDoneMsg{stats: stats, err: err}
	}
}
