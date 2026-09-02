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
	modeTasks
	modeTaskDetail
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
	args     string // tool input (blockTool only)
	output   string // tool result (blockTool only)
	toolDone bool
	final    bool
	queued   bool // blockUser only: submitted mid-run, not yet acked by the agent
	cache    string
	// joins has one textselection break kind per cache line: which lines are
	// soft-wrap continuations, so copying joins them like the terminal does.
	joins []textselection.LineBreak
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
	// lineBreaks mirrors the viewport content lines (see rebuild).
	lineBreaks []textselection.LineBreak

	dirty        bool
	flushPending bool
	expandTools  bool
	sel          textselection.Selection
	// toast is a transient confirmation at the title row's right edge (e.g.
	// "Copied N chars"); toastSeq invalidates stale clear ticks.
	toast    string
	toastSeq int
	// selAutoScroll scrolls the conversation while a drag selection sits in
	// the top/bottom edge band; selSeq invalidates stale ticks on a new drag.
	selAutoScroll textselection.AutoScroll
	selSeq        int

	md *markdown

	models  []ModelEntry
	pCursor int
	form    providerForm

	sessionID   string
	saveSeq     int
	sessionList []SessionEntry
	sCursor     int
	sFilter     string

	// history holds submitted inputs for up/down recall; histIdx is the
	// browse position (-1 = not browsing) and histDraft preserves the input
	// the user was typing when browsing started.
	history   []string
	histIdx   int
	histDraft string

	taskList     []TaskEntry
	tCursor      int
	tasksSeq     int
	taskDetailID string
	dOffset      int

	// slashCursor is the highlighted row of the slash-command menu;
	// slashMenuOff dismisses it (esc) until the input changes.
	slashCursor  int
	slashMenuOff bool

	voiceActive bool // recording indicator in the title (voice input)

	// injected holds programmatic user messages (cron wakes) buffered while a
	// picker mode is open; flushed on return to chat.
	injected []string
}

func New(runner AgentRunner, store ProviderStore, sessions SessionStore) *Model {
	in := textarea.New()
	in.Placeholder = "Ask the AI to read tabs, send keys, manage daemons..."
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.SetHeight(3)
	// The real terminal cursor follows the caret (see View); the virtual
	// inverse-video caret would double-render it.
	in.SetVirtualCursor(false)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return &Model{
		runner:   runner,
		store:    store,
		sessions: sessions,
		input:    in,
		spinner:  sp,
		status:   statusIdle,
		histIdx:  -1,
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

// layout returns the panel geometry. The AI panel renders fullscreen: the
// border box fills the frame exactly (boxW x boxH), so overlay mouse coords
// map 1:1 (overlayBounds centers a same-sized box at offset 0,0). lipgloss
// Width/Height include border and padding, so the interior wrap width is
// boxW-2(border)-2(padding).
func (m *Model) layout() (boxW, boxH, contentW, viewH int) {
	boxW = m.width
	if boxW < 20 {
		boxW = 20
	}
	boxH = m.height
	if boxH < 8 {
		boxH = 8
	}
	contentW = boxW - 4
	// Non-viewport rows: border(2) + title(1) + blank(1) + status(1) +
	// input box(5) + last row(1).
	viewH = boxH - 11
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

// SetVoiceActive toggles the recording indicator in the title bar.
func (m *Model) SetVoiceActive(v bool) { m.voiceActive = v }

// InsertText appends dictated text to the chat input (voice delivery).
func (m *Model) InsertText(text string) {
	if m.mode != modeChat {
		return
	}
	m.input.SetValue(m.input.Value() + text)
}

// SubmitInput sends the current input as if enter was pressed (voice
// sentence-end "enter" with the panel open).
func (m *Model) SubmitInput() tea.Cmd {
	if m.mode != modeChat {
		return nil
	}
	return m.send()
}

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
		m.pushHistory(prompt)
		return m.runSlashCommand(prompt)
	}
	cmd, ok := m.queueOrRun(prompt)
	if ok {
		m.pushHistory(prompt)
		m.input.Reset()
	}
	return cmd
}

// queueOrRun delivers prompt as a user message: queued onto the active run
// (dim Queued marker, acked by EventSteer) or starting a new run when idle.
// ok is false only when the enqueue failed, so the caller can keep its draft.
func (m *Model) queueOrRun(prompt string) (cmd tea.Cmd, ok bool) {
	if m.status == statusRunning {
		// Queue instead of blocking: the agent injects it at the next step
		// boundary and acks with EventSteer, which undims the block.
		if err := m.runner.Enqueue(prompt); err != nil {
			m.errMsg = "queue failed: " + err.Error()
			return nil, false
		}
		m.blocks = append(m.blocks, block{kind: blockUser, text: prompt, queued: true})
		m.collapseThinking(len(m.blocks) - 2)
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
		return nil, true
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.status = statusRunning
	m.errMsg = ""
	m.blocks = append(m.blocks, block{kind: blockUser, text: prompt})
	m.collapseThinking(len(m.blocks) - 2)
	m.renderBlock(len(m.blocks) - 1)
	m.rebuild()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch, err := m.runner.Run(ctx, prompt)
	if err != nil {
		m.status = statusError
		m.errMsg = err.Error()
		cancel()
		return nil, true
	}
	m.events = ch
	return tea.Batch(waitEvent(ch), m.spinner.Tick), true
}

// maxInjectedBuffer bounds programmatic messages buffered while a picker
// mode is open; oldest are dropped beyond it (latest state wins).
const maxInjectedBuffer = 10

// InjectUserMessage delivers a programmatic user message (e.g. a cron wake)
// without touching the draft input and without slash-command interception:
// queued onto the active run or starting a new one, same as send(). While a
// picker mode is open the message is buffered and flushed on return to chat.
func (m *Model) InjectUserMessage(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if m.mode != modeChat {
		if len(m.injected) >= maxInjectedBuffer {
			m.injected = m.injected[1:]
		}
		m.injected = append(m.injected, text)
		return nil
	}
	cmd, _ := m.queueOrRun(text)
	return cmd
}

// flushInjected delivers buffered programmatic messages once the panel is
// back in chat mode.
func (m *Model) flushInjected() tea.Cmd {
	if m.mode != modeChat || len(m.injected) == 0 {
		return nil
	}
	msgs := m.injected
	m.injected = nil
	var cmds []tea.Cmd
	for _, text := range msgs {
		if cmd, _ := m.queueOrRun(text); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) clearSession() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.runner.ClearQueue()
	m.blocks = nil
	m.events = nil
	m.injected = nil
	m.dirty = false
	m.status = statusIdle
	m.errMsg = ""
	m.sel = textselection.Selection{}
	m.selAutoScroll.Stop()
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
		name := ev.ToolName
		if name == "" {
			name = ev.Text
		}
		m.blocks = append(m.blocks, block{kind: blockTool, text: name, args: ev.ToolArgs})
		m.collapseThinking(len(m.blocks) - 2)
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
		m.collapseThinking(len(m.blocks) - 2)
	}
	return &m.blocks[len(m.blocks)-1]
}

// collapseThinking re-renders block i when it is a thinking block that just
// stopped being the stream tail, folding its live tail-window into the
// collapsed head view.
func (m *Model) collapseThinking(i int) {
	if i >= 0 && m.blocks[i].kind == blockThinking {
		m.renderBlock(i)
	}
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
	var logical []string // unwrapped source lines; nil means all real breaks
	switch b.kind {
	case blockUser:
		prefix := "You: "
		if b.queued {
			prefix = "Queued: "
		}
		logical = strings.Split(prefix+b.text, "\n")
		if b.queued {
			b.cache = ui.DimStyle.Width(cw).Render(prefix + b.text)
			break
		}
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Width(cw).
			Render(prefix + b.text)
	case blockThinking:
		lines := strings.Split(strings.TrimRight(b.text, "\n"), "\n")
		// Live (stream tail) shows a fixed tail window that scrolls with the
		// deltas; sealed blocks collapse to the head plus a hint; ctrl+o
		// (expandTools) shows everything.
		live := i == len(m.blocks)-1 && m.status == statusRunning
		shown := lines
		hint := ""
		if !m.expandTools {
			switch {
			case live && len(lines) > 2:
				shown = lines[len(lines)-2:]
			case !live && len(lines) > 2:
				shown = lines[:2]
				hint = fmt.Sprintf("... (%d more lines, ctrl+o to expand)", len(lines)-2)
			}
		}
		text := "Thinking: " + strings.Join(shown, "\n")
		if hint != "" {
			text += "\n" + hint
		}
		logical = strings.Split(text, "\n")
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Italic(true).Width(cw).
			Render(text)
	case blockSystem:
		logical = strings.Split(b.text, "\n")
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
		lines := []string{head}
		args := ""
		if b.args != "" {
			args = truncateCells(strings.Join(strings.Fields(b.args), " "), max(0, cw))
			lines = append(lines, ui.DimStyle.Render(args))
		}
		out := strings.TrimRight(b.output, "\n")
		if out == "" {
			b.cache = strings.Join(lines, "\n")
			break
		}
		if m.expandTools {
			b.cache = strings.Join(append(lines, ui.DimStyle.Width(cw).Render(out)), "\n")
			logical = []string{ansi.Strip(head)}
			if args != "" {
				logical = append(logical, args)
			}
			logical = append(logical, strings.Split(out, "\n")...)
			break
		}
		outLines := strings.Split(out, "\n")
		preview := outLines
		if len(preview) > 3 {
			preview = preview[:3]
		}
		for i, l := range preview {
			preview[i] = truncateCells(l, max(0, cw))
		}
		summary := strings.Join(preview, "\n")
		if len(outLines) > 3 {
			summary += fmt.Sprintf("\n... (%d more lines, ctrl+o to expand)", len(outLines)-3)
		}
		b.cache = strings.Join(append(lines, ui.DimStyle.Render(summary)), "\n")
	case blockAssistant:
		final := b.final || m.status != statusRunning
		out := m.md.render(b.text, final)
		// Glamour does not break overlong words (URLs); hard-wrap so no
		// line exceeds the box interior width.
		b.cache = strings.Trim(lipgloss.NewStyle().Width(cw).Render(out), "\n")
		logical = m.md.renderLogical(b.text, final)
		if final {
			b.final = true
		}
	}
	b.joins = alignBreaks(strings.Split(ansi.Strip(b.cache), "\n"), logical)
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
	// lineBreaks tracks each content line's break kind to the previous line,
	// so a copy joins soft-wrapped lines like the terminal does.
	breaks := []textselection.LineBreak{{Kind: textselection.BreakNewline}}
	for i := range m.blocks {
		parts = append(parts, m.blocks[i].cache)
		n := strings.Count(m.blocks[i].cache, "\n") + 1
		for k := 1; k < n; k++ {
			br := textselection.LineBreak{Kind: textselection.BreakNewline}
			if k < len(m.blocks[i].joins) {
				br = m.blocks[i].joins[k]
			}
			breaks = append(breaks, br)
		}
		if i < len(m.blocks)-1 {
			// The blank separator line and the next block's first line.
			breaks = append(breaks,
				textselection.LineBreak{Kind: textselection.BreakNewline},
				textselection.LineBreak{Kind: textselection.BreakNewline})
		}
	}
	content := strings.Join(parts, "\n\n")
	if m.sel.Active {
		lines := strings.Split(content, "\n")
		for i := range lines {
			lines[i] = m.sel.RenderLine(lines[i], i)
		}
		content = strings.Join(lines, "\n")
	}
	m.lineBreaks = breaks
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// contentPoint maps overlay-local mouse coords to a conversation content
// line and column: the viewport starts below border+title+blank (row 3)
// and right of border+padding (col 2).
func (m *Model) contentPoint(x, y int) (line, col int) {
	vh := m.viewport.Height()
	if vh < 1 {
		vh = 1
	}
	row := y - 3
	if row < 0 {
		row = 0
	}
	if row > vh-1 {
		row = vh - 1
	}
	return m.viewport.YOffset() + row, x - 2
}

// selectionAutoScrollMsg is the tick that scrolls the conversation while a
// drag selection sits in the top/bottom edge band.
type selectionAutoScrollMsg struct{ seq int }

const selectionAutoScrollInterval = 60 * time.Millisecond

// toastClearMsg expires the title-row toast.
type toastClearMsg struct{ seq int }

const toastDuration = 3 * time.Second

// setToast shows a transient confirmation at the title row's right edge.
func (m *Model) setToast(text string) tea.Cmd {
	m.toast = text
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(toastDuration, func(time.Time) tea.Msg { return toastClearMsg{seq: seq} })
}

func (m *Model) queueSelectionAutoScroll() tea.Cmd {
	if m.selAutoScroll.Queued || m.selAutoScroll.Dir == 0 {
		return nil
	}
	m.selAutoScroll.Queued = true
	seq := m.selSeq
	return tea.Tick(selectionAutoScrollInterval, func(time.Time) tea.Msg {
		return selectionAutoScrollMsg{seq: seq}
	})
}

// scrollSelectionOnce scrolls the conversation one row in the auto-scroll
// direction and extends the caret to the new edge line. False means the
// viewport cannot scroll further.
func (m *Model) scrollSelectionOnce() bool {
	vh := m.viewport.Height()
	switch m.selAutoScroll.Dir {
	case -1:
		if m.viewport.YOffset() <= 0 {
			return false
		}
		m.viewport.ScrollUp(1)
		m.sel.Move(m.viewport.YOffset(), m.sel.Caret.Col)
	case 1:
		if m.viewport.AtBottom() {
			return false
		}
		m.viewport.ScrollDown(1)
		m.sel.Move(m.viewport.YOffset()+vh-1, m.sel.Caret.Col)
	default:
		return false
	}
	return true
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	// Mode transitions land in update; flush buffered cron wakes when a
	// picker closed back into chat.
	return model, tea.Batch(cmd, m.flushInjected())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case tasksTickMsg:
		return m, m.tasksTick(msg.seq)
	case flushTickMsg:
		m.flush()
		return m, nil
	case selectionAutoScrollMsg:
		m.selAutoScroll.Queued = false
		if msg.seq != m.selSeq || !m.sel.Dragging {
			return m, nil
		}
		if !m.scrollSelectionOnce() {
			m.selAutoScroll.Dir = 0
			return m, nil
		}
		m.rebuild()
		return m, m.queueSelectionAutoScroll()
	case toastClearMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil
	case editorDoneMsg:
		if msg.err != nil {
			m.slashError("editor: " + msg.err.Error())
			return m, nil
		}
		m.input.SetValue(msg.text)
		return m, nil
	case compactDoneMsg:
		if msg.err != nil {
			m.slashError("compact: " + msg.err.Error())
			return m, nil
		}
		m.blocks = append(m.blocks, block{kind: blockSystem, text: fmt.Sprintf("Compaction complete (%d → %d tokens, %d → %d messages)",
			msg.stats.TokensBefore, msg.stats.TokensAfter, msg.stats.MessagesBefore, msg.stats.MessagesAfter)})
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
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
			m.selAutoScroll.Stop()
			m.selSeq++
			m.sel.Begin(m.contentPoint(msg.X, msg.Y))
			m.rebuild()
		}
		return m, nil
	case tea.MouseMotionMsg:
		if m.sel.Dragging {
			m.sel.Move(m.contentPoint(msg.X, msg.Y))
			m.rebuild()
			vh := m.viewport.Height()
			vy := min(max(msg.Y-3, 0), vh-1)
			if m.selAutoScroll.Update(vy, vh) {
				return m, m.queueSelectionAutoScroll()
			}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.sel.Dragging {
			m.selAutoScroll.Stop()
			m.selSeq++
			if m.sel.End(m.contentPoint(msg.X, msg.Y)) {
				m.rebuild()
				text := m.sel.TextJoined(strings.Split(m.viewport.GetContent(), "\n"), m.lineBreaks)
				if text != "" {
					// Toast inside the panel: the app-level toast renders
					// behind the fullscreen overlay.
					return m, tea.Batch(
						tea.SetClipboard(text),
						m.setToast(fmt.Sprintf("Copied %d chars", len([]rune(text)))),
					)
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
			m.slashMenuOff = false
			m.slashCursor = 0
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
		case modeTasks:
			return m, m.updateTasks(msg)
		case modeTaskDetail:
			return m, m.updateTaskDetail(msg)
		}
		return m.chatKey(msg)
	}
	return m, nil
}

func (m *Model) chatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// History browsing owns up/down (a recalled "/..." must not open the
	// slash menu).
	if m.histIdx >= 0 {
		switch msg.String() {
		case "up":
			m.recallUp()
			return m, nil
		case "down":
			m.recallDown()
			return m, nil
		}
	}
	if matches := m.slashMatches(); len(matches) > 0 {
		if m.slashCursor >= len(matches) {
			m.slashCursor = len(matches) - 1
		}
		switch msg.String() {
		case "up":
			if m.slashCursor > 0 {
				m.slashCursor--
			}
			return m, nil
		case "down":
			if m.slashCursor < len(matches)-1 {
				m.slashCursor++
			}
			return m, nil
		case "tab", "right":
			m.input.SetValue(matches[m.slashCursor].name)
			m.slashCursor = 0
			return m, nil
		case "enter":
			// A full command submits; anything else completes first.
			if strings.TrimSpace(m.input.Value()) != matches[m.slashCursor].name {
				m.input.SetValue(matches[m.slashCursor].name)
				m.slashCursor = 0
				return m, nil
			}
		case "esc":
			m.slashMenuOff = true
			return m, nil
		}
	}
	switch msg.String() {
	case "esc":
		m.sel = textselection.Selection{}
		m.selAutoScroll.Stop()
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
			at := len(m.blocks)
			m.blocks = append(m.blocks, block{kind: blockSystem, text: "Interrupted by user (ctrl+c)"})
			if dropped > 0 {
				m.blocks = append(m.blocks, block{kind: blockSystem, text: fmt.Sprintf("%d queued message(s) discarded", dropped)})
			}
			for i := at; i < len(m.blocks); i++ {
				m.renderBlock(i)
			}
			m.events = nil
			m.rebuild()
			return m, m.scheduleSave()
		}
		m.input.SetValue("")
		m.histIdx = -1
		m.histDraft = ""
		return m, nil
	case "ctrl+o":
		m.expandTools = !m.expandTools
		for i := range m.blocks {
			if m.blocks[i].kind == blockTool || m.blocks[i].kind == blockThinking {
				m.renderBlock(i)
			}
		}
		m.rebuild()
		return m, nil
	case "ctrl+p":
		return m, m.openProviders()
	case "ctrl+g":
		return m, m.openEditor()
	case "enter":
		return m, m.send()
	case "up":
		if strings.TrimSpace(m.input.Value()) == "" {
			m.recallUp()
			return m, nil
		}
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	}
	var cmd tea.Cmd
	old := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != old {
		// Any edit re-arms the slash menu and resets its cursor.
		m.slashMenuOff = false
		m.slashCursor = 0
	}
	return m, cmd
}

func (m *Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}
	var content string
	var cursor *tea.Cursor
	switch m.mode {
	case modeProviders:
		content = m.providersView()
	case modeProviderForm:
		content = m.form.view()
	case modeSessions:
		content = m.sessionsView()
	case modeTasks:
		content = m.tasksView()
	case modeTaskDetail:
		content = m.taskDetailView()
	default:
		content, cursor = m.chatView()
	}
	boxW, boxH, _, _ := m.layout()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Width(boxW).
		Height(boxH).
		Render(content)
	v := tea.NewView(box)
	v.Cursor = cursor
	return v
}

// humanizeTokens abbreviates a token count for the title bar: 950 -> "950",
// 104448 -> "104k", 1048576 -> "1M".
func humanizeTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func (m *Model) chatView() (string, *tea.Cursor) {
	m.syncViewportHeight()
	cw := m.contentWidth()

	// Title: name, voice recording indicator, copy toast. The model/context
	// summary lives at the bottom-right corner instead.
	title := ui.TitleStyle.Render("AI Assistant")
	if m.voiceActive {
		title += " " + ui.ErrorStyle.Render("REC")
	}
	if m.toast != "" {
		t := ui.SuccessStyle.Render(m.toast)
		if gap := cw - lipgloss.Width(title) - lipgloss.Width(t); gap > 0 {
			title += strings.Repeat(" ", gap) + t
		}
	}

	// The last row carries the error (left) and the model/context summary
	// (right, dim); it never adds a row.
	right := ""
	if m.store != nil && m.store.Active() != "" {
		right = m.store.Active()
	}
	if cu, ok := m.runner.(interface{ ContextUsage() (int, int) }); ok {
		if used, max := cu.ContextUsage(); max > 0 {
			if right != "" {
				right += " · "
			}
			right += fmt.Sprintf("context: %d%% (%s/%s)", used*100/max, humanizeTokens(used), humanizeTokens(max))
		}
	}
	right = ui.DimStyle.Render(truncateCells(right, max(0, cw)))
	rightW := lipgloss.Width(right)
	errLine := ""
	if m.errMsg != "" {
		collapsed := strings.Join(strings.Fields(m.errMsg), " ")
		errLine = ui.ErrorStyle.Render(truncateCells("error: "+collapsed, max(0, cw-rightW-1)))
	}
	lastRow := errLine
	if gap := cw - lipgloss.Width(errLine) - rightW; rightW > 0 && gap >= 0 {
		lastRow = errLine + strings.Repeat(" ", gap) + right
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

	// The slash menu sits between the conversation and the input box; its
	// rows come out of the viewport height, so the frame size is unchanged.
	menu := m.slashMenuView()
	menuRows := 0
	if menu != "" {
		menuRows = strings.Count(menu, "\n") + 1
	}

	// The running indicator reuses the blank row above the input box.
	statusRow := ""
	if m.status == statusRunning {
		statusRow = m.spinner.View()
	}

	// Real cursor at the textarea caret: outer border(1)+padding(1), then
	// title+blank+body+menu+status above the input box, then its border(1)+padding(1).
	var cursor *tea.Cursor
	if c := m.input.Cursor(); c != nil {
		c.X += 4
		c.Y += lipgloss.Height(body) + menuRows + 5
		cursor = c
	}

	rows := []string{title, "", body}
	if menu != "" {
		rows = append(rows, menu)
	}
	rows = append(rows, statusRow, inputBox, lastRow)
	return lipgloss.JoinVertical(lipgloss.Left, rows...), cursor
}

// syncViewportHeight sizes the conversation viewport to the layout height
// minus the slash menu rows currently shown. A shrink while scrolled to the
// bottom keeps the latest content in view.
func (m *Model) syncViewportHeight() {
	if m.viewport.Width() == 0 {
		return
	}
	_, _, _, vh := m.layout()
	h := vh - len(m.slashMatches())
	if h < 1 {
		h = 1
	}
	if h == m.viewport.Height() {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetHeight(h)
	if atBottom {
		m.viewport.GotoBottom()
	}
}
