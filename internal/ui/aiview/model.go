package aiview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

const streamFlushInterval = 50 * time.Millisecond

type status int

const (
	statusIdle status = iota
	statusRunning
	statusError
)

type mode int

const (
	modeChat mode = iota
	modeProviders
	modeProviderForm
	modeSessions
)

type CloseMsg struct{}

type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockThinking
	blockTool
	blockSystem
)

type block struct {
	kind     blockKind
	text     string
	output   string // tool result (blockTool only)
	toolDone bool
	final    bool
	queued   bool // blockUser only: submitted mid-run, not yet acked by the agent
	cache    string
}

type agentEventMsg struct{ ev AgentEvent }
type streamClosedMsg struct{}
type flushTickMsg struct{}
type saveTickMsg struct{ seq int }

type Model struct {
	runner   AgentRunner
	store    ProviderStore
	sessions SessionStore

	width  int
	height int
	status status
	mode   mode
	errMsg string

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	blocks []block
	cancel context.CancelFunc
	events <-chan AgentEvent

	dirty        bool
	flushPending bool
	expandTools  bool
	sel          textselection.Selection

	md *markdown

	models  []ModelEntry
	pCursor int
	form    providerForm

	sessionID   string
	saveSeq     int
	sessionList []SessionEntry
	sCursor     int
}

func New(runner AgentRunner, store ProviderStore, sessions SessionStore) *Model {
	in := textarea.New()
	in.Placeholder = "Ask the AI to read tabs, send keys, manage daemons..."
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.SetHeight(3)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return &Model{
		runner:   runner,
		store:    store,
		sessions: sessions,
		input:    in,
		spinner:  sp,
		status:   statusIdle,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) SetSize(w, h int) {
	if w == m.width && h == m.height {
		return
	}
	m.width = w
	m.height = h
	_, _, cw, vh := m.layout()
	// The input box is cw-2 wide total; its interior wrap is cw-6.
	m.input.SetWidth(cw - 6)
	m.viewport = viewport.New(viewport.WithWidth(cw), viewport.WithHeight(vh))
	if m.md == nil || m.md.width != cw {
		m.md = newMarkdown(cw)
	}
	m.renderAll()
}

func (m *Model) layout() (boxW, boxH, contentW, viewH int) {
	// The AI panel renders fullscreen; the border box fills the frame.
	boxW = m.width
	if boxW < 20 {
		boxW = 20
	}
	boxH = m.height
	if boxH < 8 {
		boxH = 8
	}
	// lipgloss v2 Width includes border and padding: the box is boxW-2 wide
	// total, so its interior wrap width is boxW-2-2(border)-2(padding).
	contentW = boxW - 6
	viewH = boxH - 2 - 1 - 5 - 1 - 3
	if viewH < 1 {
		viewH = 1
	}
	return boxW, boxH, contentW, viewH
}

func (m *Model) contentWidth() int {
	_, _, cw, _ := m.layout()
	return cw
}

func (m *Model) Running() bool { return m.status == statusRunning }

func waitEvent(ch <-chan AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return agentEventMsg{ev: ev}
	}
}

func (m *Model) send() tea.Cmd {
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		return nil
	}
	if strings.HasPrefix(prompt, "/") {
		return m.runSlashCommand(prompt)
	}
	if m.status == statusRunning {
		// Queue instead of blocking: the agent injects it at the next step
		// boundary and acks with EventSteer, which undims the block.
		m.input.Reset()
		m.runner.Enqueue(prompt)
		m.blocks = append(m.blocks, block{kind: blockUser, text: prompt, queued: true})
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
		return nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.input.Reset()
	m.status = statusRunning
	m.errMsg = ""
	m.blocks = append(m.blocks, block{kind: blockUser, text: prompt})
	m.renderBlock(len(m.blocks) - 1)
	m.rebuild()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch, err := m.runner.Run(ctx, prompt)
	if err != nil {
		m.status = statusError
		m.errMsg = err.Error()
		cancel()
		return nil
	}
	m.events = ch
	return tea.Batch(waitEvent(ch), m.spinner.Tick)
}

func (m *Model) clearSession() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.runner.ClearQueue()
	m.blocks = nil
	m.events = nil
	m.dirty = false
	m.status = statusIdle
	m.errMsg = ""
	m.sel = textselection.Selection{}
	m.sessionID = ""
	m.viewport.SetContent("")
}

// handleEvent applies one agent event; returns true when the stream is terminal.
func (m *Model) handleEvent(ev AgentEvent) bool {
	switch ev.Kind {
	case EventTextDelta:
		b := m.openBlock(blockAssistant)
		b.text += ev.Text
		m.dirty = true
	case EventThinkingDelta:
		b := m.openBlock(blockThinking)
		b.text += ev.Text
		m.dirty = true
	case EventToolCallStart:
		m.sealStream()
		m.blocks = append(m.blocks, block{kind: blockTool, text: ev.Text})
		m.renderBlock(len(m.blocks) - 1)
		m.dirty = true
	case EventToolCallEnd:
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool && !m.blocks[i].toolDone {
				m.blocks[i].toolDone = true
				m.blocks[i].output = ev.Text
				m.renderBlock(i)
				m.dirty = true
				break
			}
		}
	case EventSteer:
		// Oldest queued block first; the agent acks in queue order.
		for i := range m.blocks {
			if m.blocks[i].kind == blockUser && m.blocks[i].queued {
				m.blocks[i].queued = false
				m.renderBlock(i)
				m.dirty = true
				break
			}
		}
	case EventDone:
		m.finish()
		return true
	case EventError:
		m.finish()
		m.status = statusError
		m.errMsg = ev.Text
		return true
	}
	return false
}

func (m *Model) openBlock(kind blockKind) *block {
	if len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].kind != kind {
		m.sealStream()
		m.blocks = append(m.blocks, block{kind: kind})
	}
	return &m.blocks[len(m.blocks)-1]
}

func (m *Model) sealStream() {
	if len(m.blocks) == 0 {
		return
	}
	last := &m.blocks[len(m.blocks)-1]
	if last.kind == blockAssistant && !last.final {
		last.final = true
		m.renderBlock(len(m.blocks) - 1)
	}
}

func (m *Model) finish() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.status = statusIdle
	m.errMsg = ""
	for i := range m.blocks {
		m.renderBlock(i)
	}
	m.rebuild()
	m.dirty = false
}

// dropQueued removes queued (not yet injected) user blocks; returns the count.
func (m *Model) dropQueued() int {
	var kept []block
	n := 0
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			n++
			continue
		}
		kept = append(kept, b)
	}
	if n > 0 {
		m.blocks = kept
	}
	return n
}

func (m *Model) flush() {
	m.flushPending = false
	if !m.dirty {
		return
	}
	for i := range m.blocks {
		if m.blocks[i].cache == "" || i == len(m.blocks)-1 {
			m.renderBlock(i)
		}
	}
	m.rebuild()
	m.dirty = false
}

// truncateCells truncates s to max terminal cells (ANSI- and wide-rune-aware).
func truncateCells(s string, max int) string {
	return ansi.Truncate(s, max, "...")
}

func (m *Model) renderBlock(i int) {
	b := &m.blocks[i]
	cw := m.contentWidth()
	switch b.kind {
	case blockUser:
		if b.queued {
			b.cache = ui.DimStyle.Width(cw).Render("Queued: " + b.text)
			break
		}
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Width(cw).
			Render("You: " + b.text)
	case blockThinking:
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Italic(true).Width(cw).
			Render("Thinking: " + b.text)
	case blockSystem:
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Italic(true).Width(cw).
			Render(b.text)
	case blockTool:
		state := ui.DimStyle.Render("running...")
		stateW := 10
		if b.toolDone {
			state = ui.SuccessStyle.Render("done")
			stateW = 4
		}
		// Bound every line to the box interior width or the outer box
		// re-wraps it and the frame grows a row.
		label := truncateCells(b.text, max(0, cw-9-stateW))
		head := ui.SelectedStyle.Render("▸ tool: "+label) + " " + state
		out := strings.TrimRight(b.output, "\n")
		if out == "" {
			b.cache = head
			break
		}
		if m.expandTools {
			b.cache = head + "\n" + ui.DimStyle.Width(cw).Render(out)
			break
		}
		lines := strings.Split(out, "\n")
		preview := lines
		if len(preview) > 3 {
			preview = preview[:3]
		}
		for i, l := range preview {
			preview[i] = truncateCells(l, max(0, cw))
		}
		summary := strings.Join(preview, "\n")
		if len(lines) > 3 {
			summary += fmt.Sprintf("\n... (%d more lines, ctrl+o to expand)", len(lines)-3)
		}
		b.cache = head + "\n" + ui.DimStyle.Render(summary)
	case blockAssistant:
		final := b.final || m.status != statusRunning
		out := m.md.render(b.text, final)
		// Glamour does not break overlong words (URLs); hard-wrap so no
		// line exceeds the box interior width.
		b.cache = strings.Trim(lipgloss.NewStyle().Width(cw).Render(out), "\n")
		if final {
			b.final = true
		}
	}
}

func (m *Model) renderAll() {
	for i := range m.blocks {
		m.renderBlock(i)
	}
	m.rebuild()
}

func (m *Model) rebuild() {
	if m.viewport.Width() == 0 {
		return
	}
	atBottom := m.viewport.AtBottom()
	parts := make([]string, 0, len(m.blocks))
	for i := range m.blocks {
		parts = append(parts, m.blocks[i].cache)
	}
	content := strings.Join(parts, "\n\n")
	if m.sel.Active {
		lines := strings.Split(content, "\n")
		for i := range lines {
			lines[i] = m.sel.RenderLine(lines[i], i)
		}
		content = strings.Join(lines, "\n")
	}
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// contentPoint maps overlay-local mouse coords to a conversation content
// line and column: the viewport starts below border+title+blank (row 3)
// and right of border+padding (col 2).
func (m *Model) contentPoint(x, y int) (line, col int) {
	_, _, _, vh := m.layout()
	row := y - 3
	if row < 0 {
		row = 0
	}
	if row > vh-1 {
		row = vh - 1
	}
	return m.viewport.YOffset() + row, x - 2
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case agentEventMsg:
		if m.events == nil {
			return m, nil
		}
		terminal := m.handleEvent(msg.ev)
		var cmds []tea.Cmd
		if !terminal {
			cmds = append(cmds, waitEvent(m.events))
		} else {
			cmds = append(cmds, m.scheduleSave())
		}
		if m.dirty && !m.flushPending {
			m.flushPending = true
			cmds = append(cmds, tea.Tick(streamFlushInterval, func(time.Time) tea.Msg { return flushTickMsg{} }))
		}
		return m, tea.Batch(cmds...)
	case streamClosedMsg:
		if m.status == statusRunning {
			m.finish()
			return m, m.scheduleSave()
		}
		return m, nil
	case saveTickMsg:
		if msg.seq == m.saveSeq {
			m.saveNow()
		}
		return m, nil
	case flushTickMsg:
		m.flush()
		return m, nil
	case spinner.TickMsg:
		if m.status == statusRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.mode != modeChat {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp, tea.MouseWheelLeft:
			m.viewport.ScrollUp(3)
		case tea.MouseWheelDown, tea.MouseWheelRight:
			m.viewport.ScrollDown(3)
		}
		return m, nil
	case tea.MouseClickMsg:
		if m.mode == modeChat && msg.Button == tea.MouseLeft {
			m.sel.Begin(m.contentPoint(msg.X, msg.Y))
			m.rebuild()
		}
		return m, nil
	case tea.MouseMotionMsg:
		if m.sel.Dragging {
			m.sel.Move(m.contentPoint(msg.X, msg.Y))
			m.rebuild()
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.sel.Dragging {
			if m.sel.End(m.contentPoint(msg.X, msg.Y)) {
				m.rebuild()
				text := m.sel.Text(strings.Split(m.viewport.GetContent(), "\n"))
				if text != "" {
					return m, tea.SetClipboard(text)
				}
				return m, nil
			}
			m.rebuild()
		}
		return m, nil
	case tea.PasteMsg:
		if m.mode == modeChat {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		switch m.mode {
		case modeProviders:
			return m, m.updateProviders(msg)
		case modeProviderForm:
			return m, m.updateProviderForm(msg)
		case modeSessions:
			return m, m.updateSessions(msg)
		}
		return m.chatKey(msg)
	}
	return m, nil
}

func (m *Model) chatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.sel = textselection.Selection{}
		m.rebuild()
		// Esc only hides the panel; the run keeps going in the background
		// (status bar shows "ai running"). ctrl+c is the interrupt.
		return m, func() tea.Msg { return CloseMsg{} }
	case "ctrl+c":
		if m.status == statusRunning {
			m.finish()
			for i := range m.blocks {
				if m.blocks[i].kind == blockTool && !m.blocks[i].toolDone {
					m.blocks[i].toolDone = true
					m.renderBlock(i)
				}
			}
			dropped := m.dropQueued()
			m.runner.ClearQueue()
			m.blocks = append(m.blocks, block{kind: blockSystem, text: "Interrupted by user (ctrl+c)"})
			if dropped > 0 {
				m.blocks = append(m.blocks, block{kind: blockSystem, text: fmt.Sprintf("%d queued message(s) discarded", dropped)})
			}
			m.renderBlock(len(m.blocks) - 1)
			m.events = nil
			m.rebuild()
			return m, m.scheduleSave()
		}
		m.input.SetValue("")
		return m, nil
	case "ctrl+l":
		m.clearSession()
		return m, nil
	case "ctrl+o":
		m.expandTools = !m.expandTools
		for i := range m.blocks {
			if m.blocks[i].kind == blockTool {
				m.renderBlock(i)
			}
		}
		m.rebuild()
		return m, nil
	case "ctrl+p":
		return m, m.openProviders()
	case "enter":
		return m, m.send()
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}
	var content string
	switch m.mode {
	case modeProviders:
		content = m.providersView()
	case modeProviderForm:
		content = m.form.view()
	case modeSessions:
		content = m.sessionsView()
	default:
		content = m.chatView()
	}
	boxW, boxH, _, _ := m.layout()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Width(boxW - 2).
		Height(boxH).
		Render(content)
	return tea.NewView(box)
}

func (m *Model) chatView() string {
	cw := m.contentWidth()
	ctx := ""
	if cu, ok := m.runner.(interface{ ContextUsage() (int, int) }); ok {
		if used, max := cu.ContextUsage(); max > 0 {
			ctx = fmt.Sprintf(" · ctx %d%%", used*100/max)
		}
	}
	title := ui.TitleStyle.Render("AI Assistant")
	if m.store != nil && m.store.Active() != "" {
		// "AI Assistant"(12) + " · "(3) + spinner(2) + ctx must fit in cw.
		label := truncateCells(m.store.Active(), max(0, cw-18-len(ctx)))
		title += ui.DimStyle.Render(" · " + label)
	}
	if m.status == statusRunning {
		title += " " + m.spinner.View()
	}
	title += ui.DimStyle.Render(ctx)

	hintText := truncateCells("enter send · /help · esc close", max(0, cw))
	hint := ui.DimStyle.Render(hintText)
	// The error takes the blank row above the hint so it never adds a row.
	errLine := ""
	if m.errMsg != "" {
		collapsed := strings.Join(strings.Fields(m.errMsg), " ")
		errLine = ui.ErrorStyle.Render(truncateCells("error: "+collapsed, max(0, cw)))
	}

	body := m.viewport.View()
	if len(m.blocks) == 0 {
		body = ui.DimStyle.Render("Ask the AI to help with your terminal session.")
	}

	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#45475A")).
		Padding(0, 1).
		Width(m.contentWidth() - 2).
		Render(m.input.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		body,
		"",
		inputBox,
		errLine,
		hint,
	)
}
