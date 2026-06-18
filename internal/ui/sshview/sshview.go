// Package sshview renders an SSH shell inside Bubble Tea (no tea.Exec), so the tab bar stays visible.
// Terminal emulation uses github.com/charmbracelet/x/vt (ANSI screen state, cursor, colors).
//
// Full-screen TUI programs (vim, less, more) run in the remote shell; they use the PTY size
// from SetSize and TERM from the session. They are not implemented in Go here—standard
// OpenSSH-style behaviour once the pty geometry matches the layout row count.
package sshview

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

var streamIDGen atomic.Uint64

// bottomPadMax is how many empty rows the user can scroll past the live bottom.
const bottomPadMax = 2

// ChunkMsg carries PTY stdout for one embedded session; StreamID routes it in App.Update.
type ChunkMsg struct {
	StreamID uint64
	Data     []byte
}

// StreamDoneMsg signals that the remote shell exited or the PTY read ended.
type StreamDoneMsg struct {
	StreamID uint64
	Err      error
}

type selectionAutoScrollMsg struct {
	StreamID uint64
}

// Model streams PTY output through a virtual terminal and forwards keys to the SSH session.
type Model struct {
	sess     *internalssh.InteractiveSession
	emu      *vt.Emulator
	streamID uint64

	alias     string
	hostID    uint
	historyID uint
	width     int
	height    int

	ch     chan []byte
	mu     sync.Mutex
	closed bool

	endErr       error
	waitComplete bool
	doneClosed   chan struct{}
	disconnected bool

	// Scrollback view: scrollOffset > 0 means viewing history.
	// 0 = live view (bottom of scrollback), N = N lines scrolled up.
	scrollOffset int

	// bottomPad > 0 lets the user scroll past the live bottom, pushing the
	// newest line up and showing empty rows below it (0..bottomPadMax).
	bottomPad int

	// Mouse drag text selection over the visible screen + scrollback.
	sel selection

	selectionAutoScrollDir    int
	selectionAutoScrollQueued bool

	// Configurable keybindings
	vk viewkeys.SSHKeys

	appCursorKeys  bool
	mouseMode      bool
	bracketedPaste bool
}

func (m *Model) SetViewKeys(vk viewkeys.SSHKeys) { m.vk = vk }

// New creates a model; call SetSize or rely on WindowSizeMsg. hostID is used to reconnect after a network drop.
func New(is *internalssh.InteractiveSession, alias string, hostID uint, vk viewkeys.SSHKeys) *Model {
	emu := vt.NewEmulator(80, 24)
	m := &Model{
		sess:       is,
		emu:        emu,
		streamID:   streamIDGen.Add(1),
		alias:      alias,
		hostID:     hostID,
		ch:         make(chan []byte, 128),
		doneClosed: make(chan struct{}),
		vk:         vk,
	}
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(mode ansi.Mode) {
			if mode == ansi.ModeCursorKeys {
				m.appCursorKeys = true
			}
			if mode == ansi.ModeBracketedPaste {
				m.bracketedPaste = true
			}
			if isMouseTrackingMode(mode) {
				m.mouseMode = true
			}
		},
		DisableMode: func(mode ansi.Mode) {
			if mode == ansi.ModeCursorKeys {
				m.appCursorKeys = false
			}
			if mode == ansi.ModeBracketedPaste {
				m.bracketedPaste = false
			}
			if isMouseTrackingMode(mode) {
				m.mouseMode = false
			}
		},
	})
	// Drain the emulator's input pipe so internal writes (e.g. in-band resize
	// responses) never block emu.Write(). Without this, vi/less freeze the app.
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := emu.Read(buf)
			if err != nil {
				return
			}
			if n > 0 && m.sess != nil && m.sess.Stdin != nil {
				_, _ = m.sess.Stdin.Write(buf[:n])
			}
		}
	}()
	return m
}

func isMouseTrackingMode(mode ansi.Mode) bool {
	switch mode {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		return true
	default:
		return false
	}
}

// StreamID identifies this session for routing ChunkMsg / StreamDoneMsg when the tab is not active.
func (m *Model) StreamID() uint64 { return m.streamID }

// HostID is the DB host used to redial after [Disconnected] is true.
func (m *Model) HostID() uint { return m.hostID }

// HistoryID returns the connection history record ID for disconnect tracking.
func (m *Model) HistoryID() uint { return m.historyID }

// SetHistoryID sets the connection history record ID.
func (m *Model) SetHistoryID(id uint) { m.historyID = id }

// PasteCommand writes a command string to the SSH session stdin.
func (m *Model) PasteCommand(cmd string) {
	if m.disconnected || m.sess == nil || m.sess.Stdin == nil {
		return
	}
	_, _ = m.sess.Stdin.Write([]byte(cmd))
}

func (m *Model) PasteText(text string) {
	m.PasteCommand(text)
}

// Disconnected is true after a network-style drop; press "r" to send [types.SSHReconnectMsg].
func (m *Model) Disconnected() bool { return m.disconnected }

// SetSize resizes the VT and notifies the remote PTY. h is mainContentHeightForType(SSHTab):
// terminal minus tab strip, divider line, status bar (with shortcut hints). Toast shares the divider row in App.
func (m *Model) SetSize(w, h int) {
	if w < 20 {
		w = 80
	}
	if h < 4 {
		h = 24
	}
	m.width, m.height = w, h
	termH := h
	if termH < 1 {
		termH = 1
	}
	m.emu.Resize(w, termH)
	if m.sess != nil && m.sess.Resize != nil {
		_ = m.sess.Resize(termH, w)
	}
}

func (m *Model) setReadErr(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if errors.Is(err, io.EOF) && m.waitComplete {
		return
	}
	if m.endErr == nil {
		m.endErr = err
		return
	}
	// Prefer a concrete error over io.EOF when both appear (read vs Wait).
	if errors.Is(m.endErr, io.EOF) && !errors.Is(err, io.EOF) {
		m.endErr = err
	}
}

func (m *Model) setWaitErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitComplete = true
	if err == nil {
		if m.endErr == nil || errors.Is(m.endErr, io.EOF) {
			m.endErr = nil
		}
		return
	}
	if m.endErr == nil || errors.Is(m.endErr, io.EOF) {
		m.endErr = err
	}
}

func (m *Model) Init() tea.Cmd {
	go m.readLoop()
	go func() {
		if m.sess == nil {
			return
		}
		err := <-m.sess.Done
		m.setWaitErr(err)
		if m.doneClosed != nil {
			close(m.doneClosed)
		}
	}()
	return waitChunk(m)
}

func (m *Model) readLoop() {
	buf := make([]byte, 8192)
	for {
		if m.sess == nil || m.sess.Stdout == nil {
			m.closeCh()
			return
		}
		n, err := m.sess.Stdout.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			m.ch <- b
		}
		if err != nil {
			m.setReadErr(err)
			m.closeCh()
			return
		}
	}
}

func (m *Model) closeCh() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.ch)
}

func waitChunk(m *Model) tea.Cmd {
	return func() tea.Msg {
		b, ok := <-m.ch
		if !ok {
			if m.doneClosed != nil {
				select {
				case <-m.doneClosed:
				case <-time.After(250 * time.Millisecond):
				}
			}
			m.mu.Lock()
			err := m.endErr
			m.mu.Unlock()
			return StreamDoneMsg{StreamID: m.streamID, Err: err}
		}
		return ChunkMsg{StreamID: m.streamID, Data: b}
	}
}

// Close ends the SSH session.
func (m *Model) Close() error {
	m.closeCh()
	_ = m.emu.Close() // stops the drain goroutine
	if m.sess != nil {
		return m.sess.Close()
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case ChunkMsg:
		if msg.StreamID != m.streamID {
			return m, nil
		}
		before := m.emu.ScrollbackLen()
		_, _ = m.emu.Write(msg.Data)
		// Only follow new output when already at the live view (bottom). When the
		// user has scrolled up, keep the same lines in view by compensating for the
		// rows that the new output pushed into scrollback.
		if m.scrollOffset > 0 {
			if added := m.emu.ScrollbackLen() - before; added > 0 {
				m.scrollOffset += added
				if maxScroll := m.emu.ScrollbackLen(); m.scrollOffset > maxScroll {
					m.scrollOffset = maxScroll
				}
			}
		}
		return m, waitChunk(m)

	case StreamDoneMsg:
		if msg.StreamID != m.streamID {
			return m, nil
		}
		err := msg.Err
		if shouldOfferReconnect(err) {
			m.disconnected = true
			if m.sess != nil {
				_ = m.sess.Close()
				m.sess = nil
			}
			alias := m.alias
			retry := tea.Msg(nil)
			if m.hostID != 0 {
				retry = types.SSHReconnectMsg{HostID: m.hostID, StreamID: m.streamID}
			}
			return m, func() tea.Msg {
				return types.ConnErrorMsg{Err: err, Target: alias, Retry: retry}
			}
		}
		return m, func() tea.Msg {
			return types.SSHDisconnectMsg{Err: err, Alias: m.alias, StreamID: m.streamID}
		}

	case tea.KeyPressMsg:
		if m.disconnected {
			if viewkeys.MatchKey(msg, m.vk.Reconnect) && m.hostID != 0 {
				hid := m.hostID
				sid := m.streamID
				return m, func() tea.Msg {
					return types.SSHReconnectMsg{HostID: hid, StreamID: sid}
				}
			}
			return m, nil
		}
		// Snippet picker
		if viewkeys.MatchKey(msg, m.vk.SnippetPicker) {
			return m, func() tea.Msg { return types.SnippetPickerRequestMsg{} }
		}
		// Any keypress snaps back to live view and clears any text selection.
		m.scrollOffset = 0
		m.bottomPad = 0
		m.sel.active = false
		if b := m.encodeKey(msg); len(b) > 0 && m.sess != nil && m.sess.Stdin != nil {
			_, _ = m.sess.Stdin.Write(b)
		}
		return m, nil

	case tea.PasteMsg:
		if m.disconnected {
			return m, nil
		}
		if m.sess != nil && m.sess.Stdin != nil {
			payload := []byte(msg.String())
			if m.bracketedPaste {
				payload = append([]byte(ansi.BracketedPasteStart), append(payload, []byte(ansi.BracketedPasteEnd)...)...)
			}
			_, _ = m.sess.Stdin.Write(payload)
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.emu.IsAltScreen() && m.sendRemoteMouse(msg) {
			return m, nil
		}
		if m.disconnected || m.emu.IsAltScreen() {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			x, y := m.clampMouse(msg.X, msg.Y)
			p := selPoint{line: m.visibleAbsLine(y), col: x}
			m.sel = selection{active: true, dragging: true, anchor: p, caret: p}
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.emu.IsAltScreen() && m.sendRemoteMouse(msg) {
			return m, nil
		}
		if !m.sel.dragging {
			return m, nil
		}
		x, y := m.clampMouse(msg.X, msg.Y)
		m.sel.caret = selPoint{line: m.visibleAbsLine(y), col: x}
		return m, m.updateSelectionAutoScroll(y)

	case tea.MouseReleaseMsg:
		if m.emu.IsAltScreen() && m.sendRemoteMouse(msg) {
			return m, nil
		}
		if !m.sel.dragging {
			return m, nil
		}
		x, y := m.clampMouse(msg.X, msg.Y)
		m.sel.caret = selPoint{line: m.visibleAbsLine(y), col: x}
		m.sel.dragging = false
		m.selectionAutoScrollDir = 0
		m.selectionAutoScrollQueued = false
		// Click without drag clears the selection; a real drag copies.
		if m.sel.anchor == m.sel.caret {
			m.sel.active = false
			return m, nil
		}
		txt := m.selectedText()
		if txt == "" {
			m.sel.active = false
			return m, nil
		}
		return m, tea.Batch(
			tea.SetClipboard(txt),
			func() tea.Msg { return types.SuccessMsg{Message: fmt.Sprintf("Copied %d chars", len([]rune(txt)))} },
		)

	case tea.MouseWheelMsg:
		if m.disconnected {
			return m, nil
		}
		// In alternate screen (vim, less), forward scroll to the remote app.
		if m.emu.IsAltScreen() {
			if m.sendRemoteMouse(msg) {
				return m, nil
			}
			if m.sess != nil && m.sess.Stdin != nil {
				lines := 3
				var seq []byte
				switch msg.Button {
				case tea.MouseWheelUp:
					seq = []byte("\x1b[A")
				case tea.MouseWheelDown:
					seq = []byte("\x1b[B")
				}
				if len(seq) > 0 {
					for i := 0; i < lines; i++ {
						_, _ = m.sess.Stdin.Write(seq)
					}
				}
			}
			return m, nil
		}
		// Normal screen: scroll through scrollback history, with up to
		// bottomPadMax extra rows of empty space below the live view.
		maxScroll := m.emu.ScrollbackLen()
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.bottomPad > 0 {
				m.bottomPad -= 3
				if m.bottomPad < 0 {
					m.bottomPad = 0
				}
			} else {
				m.scrollOffset += 3
				if m.scrollOffset > maxScroll {
					m.scrollOffset = maxScroll
				}
			}
		case tea.MouseWheelDown:
			if m.scrollOffset > 0 {
				m.scrollOffset -= 3
				if m.scrollOffset < 0 {
					m.scrollOffset = 0
				}
			} else {
				m.bottomPad += 3
				if m.bottomPad > bottomPadMax {
					m.bottomPad = bottomPadMax
				}
			}
		}
		return m, nil

	case selectionAutoScrollMsg:
		if msg.StreamID != m.streamID {
			return m, nil
		}
		m.selectionAutoScrollQueued = false
		if !m.sel.dragging || m.selectionAutoScrollDir == 0 {
			return m, nil
		}
		if !m.scrollSelectionOnce() {
			m.selectionAutoScrollDir = 0
			return m, nil
		}
		return m, m.queueSelectionAutoScroll()
	}

	return m, nil
}

func (m *Model) updateSelectionAutoScroll(y int) tea.Cmd {
	h := m.emu.Height()
	if h <= 0 {
		return nil
	}
	switch {
	case y <= 0:
		m.selectionAutoScrollDir = -1
	case y >= h-1:
		m.selectionAutoScrollDir = 1
	default:
		m.selectionAutoScrollDir = 0
		return nil
	}
	return m.queueSelectionAutoScroll()
}

func (m *Model) queueSelectionAutoScroll() tea.Cmd {
	if m.selectionAutoScrollQueued || m.selectionAutoScrollDir == 0 {
		return nil
	}
	m.selectionAutoScrollQueued = true
	streamID := m.streamID
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg {
		return selectionAutoScrollMsg{StreamID: streamID}
	})
}

func (m *Model) scrollSelectionOnce() bool {
	switch m.selectionAutoScrollDir {
	case -1:
		if m.bottomPad > 0 {
			m.bottomPad--
		} else if maxScroll := m.emu.ScrollbackLen(); m.scrollOffset < maxScroll {
			m.scrollOffset++
		} else {
			return false
		}
		m.sel.caret.line = m.visibleAbsLine(0)
	case 1:
		h := m.emu.Height()
		if m.scrollOffset > 0 {
			m.scrollOffset--
		} else if m.bottomPad < bottomPadMax {
			m.bottomPad++
		} else {
			return false
		}
		m.sel.caret.line = m.visibleAbsLine(h - 1)
	default:
		return false
	}
	return true
}

func (m *Model) sendRemoteMouse(msg tea.Msg) bool {
	if m.disconnected || !m.mouseMode {
		return false
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mm := uv.Mouse(msg.Mouse())
		m.emu.SendMouse(vt.MouseClick(mm))
	case tea.MouseMotionMsg:
		mm := uv.Mouse(msg.Mouse())
		m.emu.SendMouse(vt.MouseMotion(mm))
	case tea.MouseReleaseMsg:
		mm := uv.Mouse(msg.Mouse())
		m.emu.SendMouse(vt.MouseRelease(mm))
	case tea.MouseWheelMsg:
		mm := uv.Mouse(msg.Mouse())
		m.emu.SendMouse(vt.MouseWheel(mm))
	default:
		return false
	}
	return true
}
