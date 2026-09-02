package aiview

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pushHistory records a submitted input and ends any history browsing.
func (m *Model) pushHistory(prompt string) {
	m.history = append(m.history, prompt)
	m.histIdx = -1
	m.histDraft = ""
}

// recallUp handles up on an empty (or browsed) input: queue recall wins while
// a run has queued messages and browsing has not started; otherwise it walks
// the input history back, preserving the in-progress draft.
func (m *Model) recallUp() {
	if m.histIdx < 0 && m.status == statusRunning {
		if text, ok := m.runner.DequeueLast(); ok {
			m.removeLastQueuedBlock()
			m.input.SetValue(text)
			return
		}
	}
	if len(m.history) == 0 {
		return
	}
	if m.histIdx < 0 {
		m.histDraft = m.input.Value()
		m.histIdx = len(m.history) - 1
	} else {
		m.saveBrowsedEdit()
		if m.histIdx > 0 {
			m.histIdx--
		}
	}
	m.input.SetValue(m.history[m.histIdx])
}

// recallDown walks the input history forward; past the newest entry it
// restores the draft that browsing started from.
func (m *Model) recallDown() {
	if m.histIdx < 0 {
		return
	}
	m.saveBrowsedEdit()
	if m.histIdx < len(m.history)-1 {
		m.histIdx++
		m.input.SetValue(m.history[m.histIdx])
		return
	}
	m.histIdx = -1
	m.input.SetValue(m.histDraft)
	m.histDraft = ""
}

// saveBrowsedEdit keeps edits made to a recalled entry when browsing moves
// away from it, so no typed text is lost.
func (m *Model) saveBrowsedEdit() {
	if v := m.input.Value(); v != m.history[m.histIdx] {
		m.history[m.histIdx] = v
	}
}

func (m *Model) removeLastQueuedBlock() {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == blockUser && m.blocks[i].queued {
			m.blocks = append(m.blocks[:i], m.blocks[i+1:]...)
			m.renderAll()
			return
		}
	}
}

type editorDoneMsg struct {
	text string
	err  error
}

// openEditor (ctrl+g) moves the draft into a temp file and opens it in
// $VISUAL/$EDITOR (default vi); the saved content replaces the input.
func (m *Model) openEditor() tea.Cmd {
	f, err := os.CreateTemp("", "eterm-aiview-*.txt")
	if err != nil {
		m.slashError("editor: " + err.Error())
		return nil
	}
	path := f.Name()
	if _, err := f.WriteString(m.input.Value()); err != nil {
		f.Close()
		os.Remove(path)
		m.slashError("editor: " + err.Error())
		return nil
	}
	f.Close()
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	args := append(strings.Fields(editor), path)
	return tea.ExecProcess(exec.Command(args[0], args[1:]...), func(err error) tea.Msg {
		defer os.Remove(path)
		data, rerr := os.ReadFile(path)
		if err == nil {
			err = rerr
		}
		return editorDoneMsg{text: strings.TrimRight(string(data), "\n"), err: err}
	})
}
