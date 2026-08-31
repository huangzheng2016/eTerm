package aiview

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
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
)

type CloseMsg struct{}

type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockThinking
	blockTool
)

type block struct {
	kind     blockKind
	text     string
	toolDone bool
	final    bool
	cache    string
}

type agentEventMsg struct{ ev AgentEvent }
type streamClosedMsg struct{}
type flushTickMsg struct{}

type Model struct {
	runner AgentRunner
	store  ProviderStore

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

	md *markdown

	providers []Provider
	pCursor   int
	form      providerForm
}

func New(runner AgentRunner, store ProviderStore) *Model {
	in := textarea.New()
	in.Placeholder = "Ask the AI..."
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.SetHeight(3)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return &Model{
		runner:  runner,
		store:   store,
		input:   in,
		spinner: sp,
		status:  statusIdle,
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
	m.input.SetWidth(cw)
	m.viewport = viewport.New(viewport.WithWidth(cw), viewport.WithHeight(vh))
	if m.md == nil || m.md.width != cw {
		m.md = newMarkdown(cw)
	}
	m.renderAll()
}

func (m *Model) layout() (boxW, boxH, contentW, viewH int) {
	boxW = m.width - 4
	if boxW > 100 {
		boxW = 100
	}
	if boxW < 20 {
		boxW = 20
	}
	boxH = m.height - 4
	if boxH > 40 {
		boxH = 40
	}
	if boxH < 8 {
		boxH = 8
	}
	contentW = boxW - 4
	viewH = boxH - 2 - 1 - 3 - 1 - 2
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
	if prompt == "" || m.status == statusRunning {
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
	m.blocks = nil
	m.events = nil
	m.dirty = false
	m.status = statusIdle
	m.errMsg = ""
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
	for i := range m.blocks {
		m.renderBlock(i)
	}
	m.rebuild()
	m.dirty = false
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

func (m *Model) renderBlock(i int) {
	b := &m.blocks[i]
	cw := m.contentWidth()
	switch b.kind {
	case blockUser:
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Width(cw).
			Render("You: " + b.text)
	case blockThinking:
		b.cache = lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Italic(true).Width(cw).
			Render("Thinking: " + b.text)
	case blockTool:
		state := ui.DimStyle.Render("running...")
		if b.toolDone {
			state = ui.SuccessStyle.Render("done")
		}
		b.cache = ui.SelectedStyle.Render("▸ tool: "+b.text) + " " + state
	case blockAssistant:
		final := b.final || m.status != statusRunning
		out := m.md.render(b.text, final)
		b.cache = strings.Trim(out, "\n")
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
	m.viewport.SetContent(strings.Join(parts, "\n\n"))
	if atBottom {
		m.viewport.GotoBottom()
	}
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
		}
		if m.dirty && !m.flushPending {
			m.flushPending = true
			cmds = append(cmds, tea.Tick(streamFlushInterval, func(time.Time) tea.Msg { return flushTickMsg{} }))
		}
		return m, tea.Batch(cmds...)
	case streamClosedMsg:
		if m.status == statusRunning {
			m.finish()
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
		}
		return m.chatKey(msg)
	}
	return m, nil
}

func (m *Model) chatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		if m.status == statusRunning {
			m.status = statusIdle
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case "ctrl+l":
		m.clearSession()
		return m, nil
	case "ctrl+p":
		if m.store == nil {
			return m, nil
		}
		m.providers = m.store.Providers()
		m.pCursor = 0
		for i, p := range m.providers {
			if p.Name == m.store.Active() {
				m.pCursor = i
			}
		}
		m.mode = modeProviders
		return m, nil
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
	default:
		content = m.chatView()
	}
	boxW, boxH, _, _ := m.layout()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Width(boxW - 2).
		Height(boxH - 2).
		Render(content)
	return tea.NewView(box)
}

func (m *Model) chatView() string {
	title := ui.TitleStyle.Render("AI Assistant")
	if m.store != nil && m.store.Active() != "" {
		title += ui.DimStyle.Render(" · " + m.store.Active())
	}
	if m.status == statusRunning {
		title += " " + m.spinner.View()
	}

	hint := ui.DimStyle.Render("enter send · pgup/pgdn scroll · ctrl+l clear · ctrl+p providers · esc close")
	if m.status == statusError {
		hint = ui.ErrorStyle.Render("error: "+m.errMsg) + "\n" + hint
	}

	body := m.viewport.View()
	if len(m.blocks) == 0 {
		body = ui.DimStyle.Render("Ask the AI to help with your terminal session.")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		body,
		"",
		m.input.View(),
		"",
		hint,
	)
}
