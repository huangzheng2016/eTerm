package aiview

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
)

const saveDebounce = 2 * time.Second

const slashHelpText = "Commands: /model pick model · /tasks background agents · /tools local shell+files · /new new session · /resume restore session · /fork fork session · /undo rewind one turn · /help this help" +
	"\nKeys: enter send · ctrl+c stop · ctrl+o tools · ctrl+p models · ctrl+l clear · pgup/pgdn scroll · drag copy · esc close"

// historyMessage is the panel's read view of exported agent history JSON
// (eino schema.Message); only user/assistant text turns rebuild into blocks.
type historyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// runSlashCommand handles input starting with "/"; it is never sent to the
// agent. Unknown commands keep the input and show an inline error.
func (m *Model) runSlashCommand(input string) tea.Cmd {
	cmd := strings.Fields(input)[0]
	switch cmd {
	case "/model", "/new", "/resume", "/fork", "/undo", "/help", "/tasks", "/tools":
	default:
		return m.slashError("unknown command " + cmd + " - try /help")
	}
	// /tasks and /tools stay available mid-run: background agents run then,
	// and a tools toggle only affects the next run's agent build.
	if m.status == statusRunning && cmd != "/help" && cmd != "/model" && cmd != "/tasks" && cmd != "/tools" {
		return m.slashError("run in progress - ctrl+c to stop")
	}
	m.input.Reset()
	if m.status == statusError {
		m.status = statusIdle
		m.errMsg = ""
	}
	switch cmd {
	case "/help":
		m.blocks = append(m.blocks, block{kind: blockSystem, text: slashHelpText})
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
	case "/model":
		return m.openProviders()
	case "/new":
		m.newSession()
	case "/resume":
		m.openSessions()
	case "/fork":
		m.forkSession()
	case "/undo":
		m.undoTurn()
	case "/tasks":
		return m.openTasks()
	case "/tools":
		m.toggleLocalTools()
	}
	return nil
}

// toggleLocalTools flips the opt-in local bash/file tools (persisted by the
// bridge) and reports the new state. The toggle keys the agent cache, so it
// takes effect on the next run.
func (m *Model) toggleLocalTools() {
	t, ok := m.runner.(interface{ ToggleLocalTools() bool })
	if !ok {
		m.slashError("local tools not supported")
		return
	}
	text := "local tools disabled (bash, str_replace_editor) - applies to new runs"
	if t.ToggleLocalTools() {
		text = "local tools enabled (bash, str_replace_editor) - applies to new runs"
	}
	m.blocks = append(m.blocks, block{kind: blockSystem, text: text})
	m.renderBlock(len(m.blocks) - 1)
	m.rebuild()
}

func (m *Model) slashError(text string) tea.Cmd {
	m.errMsg = text
	if m.status == statusIdle {
		m.status = statusError
	}
	return nil
}

func (m *Model) openProviders() tea.Cmd {
	if m.store == nil {
		return nil
	}
	m.models = m.store.Models()
	m.pCursor = 0
	for i, e := range m.models {
		if e.Label == m.store.Active() {
			m.pCursor = i
		}
	}
	m.mode = modeProviders
	return nil
}

func (m *Model) newSession() {
	m.saveNow() // flush a pending autosave before abandoning the session
	m.clearSession()
	if m.sessions != nil {
		m.sessions.ResetHistory()
	}
	m.blocks = append(m.blocks, block{kind: blockSystem, text: "new session started"})
	m.renderBlock(0)
	m.rebuild()
}

func (m *Model) forkSession() {
	title := m.sessionTitle()
	if title == "" {
		m.slashError("nothing to fork yet")
		return
	}
	m.saveNow() // flush a pending autosave so the parent keeps its last turn
	newID := newSessionID()
	m.sessions.SaveSession(newID, title, m.sessionID)
	m.sessionID = newID
	m.blocks = append(m.blocks, block{kind: blockSystem, text: "forked into a new session"})
	m.renderBlock(len(m.blocks) - 1)
	m.rebuild()
}

func (m *Model) undoTurn() {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == blockUser {
			m.blocks = m.blocks[:i]
			m.sessions.UndoLastTurn()
			m.renderAll()
			m.saveNow()
			return
		}
	}
	m.slashError("nothing to undo")
}

func (m *Model) openSessions() {
	m.sessionList = m.sessions.Sessions()
	if len(m.sessionList) == 0 {
		m.slashError("no saved sessions")
		return
	}
	m.sCursor = 0
	m.mode = modeSessions
}

func (m *Model) loadSession(e SessionEntry) {
	m.saveNow() // flush a pending autosave before switching sessions
	data, ok := m.sessions.LoadSession(e.ID)
	if !ok {
		m.mode = modeChat
		m.slashError("session could not be loaded")
		return
	}
	m.clearSession()
	m.mode = modeChat
	m.sessionID = e.ID
	m.blocks = append(m.blocks, block{kind: blockSystem, text: "restored session: " + e.Title})
	var msgs []historyMessage
	if err := json.Unmarshal(data, &msgs); err == nil {
		for _, msg := range msgs {
			switch msg.Role {
			case "user":
				m.blocks = append(m.blocks, block{kind: blockUser, text: msg.Content})
			case "assistant":
				if msg.Content != "" {
					m.blocks = append(m.blocks, block{kind: blockAssistant, text: msg.Content, final: true})
				}
			}
		}
	}
	m.renderAll()
}

func (m *Model) updateSessions(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeChat
	case "up", "k":
		if m.sCursor > 0 {
			m.sCursor--
		}
	case "down", "j":
		if m.sCursor < len(m.sessionList)-1 {
			m.sCursor++
		}
	case "enter":
		if m.sCursor < len(m.sessionList) {
			m.loadSession(m.sessionList[m.sCursor])
		}
	}
	return nil
}

func (m *Model) sessionsView() string {
	rows := []string{ui.TitleStyle.Render("Sessions"), ""}
	for i, e := range m.sessionList {
		cursor := "  "
		style := ui.DimStyle
		if i == m.sCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		detail := e.Provider
		if e.Model != "" && e.Model != e.Provider {
			detail += "/" + e.Model
		}
		detail += " · " + e.UpdatedAt.Format("01-02 15:04")
		line := fmt.Sprintf("%s%s %s", cursor, style.Render(e.Title), ui.DimStyle.Render("["+detail+"]"))
		rows = append(rows, truncateCells(line, max(0, m.contentWidth())))
	}
	rows = append(rows, "",
		ui.DimStyle.Render("enter restore | esc back"))
	return strings.Join(rows, "\n")
}

// saveNow persists the current session via the bridge. The session id is
// allocated lazily on first save; conversations without a user turn are not
// persisted, but an existing row is always updated (e.g. emptied by /undo).
func (m *Model) saveNow() {
	if m.sessions == nil {
		return
	}
	title := m.sessionTitle()
	if m.sessionID == "" {
		if title == "" {
			return
		}
		m.sessionID = newSessionID()
	}
	m.sessions.SaveSession(m.sessionID, title, "")
}

// scheduleSave debounces autosave to 2s after the latest run end.
func (m *Model) scheduleSave() tea.Cmd {
	if m.sessions == nil {
		return nil
	}
	m.saveSeq++
	seq := m.saveSeq
	return tea.Tick(saveDebounce, func(time.Time) tea.Msg { return saveTickMsg{seq: seq} })
}

func (m *Model) sessionTitle() string {
	for _, b := range m.blocks {
		if b.kind == blockUser {
			return truncateCells(strings.Join(strings.Fields(b.text), " "), 60)
		}
	}
	return ""
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}
