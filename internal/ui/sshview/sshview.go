// Package sshview renders an SSH shell inside Bubble Tea (no tea.Exec), so the tab bar stays visible.
// Terminal emulation uses github.com/charmbracelet/x/vt (ANSI screen state, cursor, colors).
//
// Full-screen TUI programs (vim, less, more) run in the remote shell; they use the PTY size
// from SetSize and TERM from the session. They are not implemented in Go here—standard
// OpenSSH-style behaviour once the pty geometry matches the layout row count.
package sshview

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/x/vt"
	tea "charm.land/bubbletea/v2"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

var streamIDGen atomic.Uint64

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
	disconnected bool

	// Scrollback view: scrollOffset > 0 means viewing history.
	// 0 = live view (bottom of scrollback), N = N lines scrolled up.
	scrollOffset int

	// Configurable keybindings
	vk viewkeys.SSHKeys
}

func (m *Model) SetViewKeys(vk viewkeys.SSHKeys) { m.vk = vk }

// New creates a model; call SetSize or rely on WindowSizeMsg. hostID is used to reconnect after a network drop.
func New(is *internalssh.InteractiveSession, alias string, hostID uint, vk viewkeys.SSHKeys) *Model {
	emu := vt.NewEmulator(80, 24)
	// Drain the emulator's input pipe so internal writes (e.g. in-band resize
	// responses) never block emu.Write(). Without this, vi/less freeze the app.
	go func() {
		buf := make([]byte, 256)
		for {
			_, err := emu.Read(buf)
			if err != nil {
				return
			}
		}
	}()
	return &Model{
		sess:     is,
		emu:      emu,
		streamID: streamIDGen.Add(1),
		alias:    alias,
		hostID:   hostID,
		ch:       make(chan []byte, 128),
		vk:       vk,
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
	if m.sess != nil && m.sess.Session != nil {
		_ = m.sess.Session.WindowChange(termH, w)
	}
}

func (m *Model) setEndErr(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.endErr == nil {
		m.endErr = err
		return
	}
	// Prefer a concrete error over io.EOF when both appear (read vs Wait).
	if errors.Is(m.endErr, io.EOF) && !errors.Is(err, io.EOF) {
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
		m.setEndErr(err)
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
			select {
			case m.ch <- b:
			default:
			}
		}
		if err != nil {
			m.setEndErr(err)
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
		_, _ = m.emu.Write(msg.Data)
		// New output arrives — snap back to live view
		m.scrollOffset = 0
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
			return m, nil
		}
		return m, func() tea.Msg {
			return types.SSHDisconnectMsg{Err: err, Alias: m.alias, StreamID: m.streamID}
		}

	case tea.KeyPressMsg:
		if m.disconnected {
			if viewkeys.MatchAny(msg.String(), m.vk.Reconnect) && m.hostID != 0 {
				hid := m.hostID
				sid := m.streamID
				return m, func() tea.Msg {
					return types.SSHReconnectMsg{HostID: hid, StreamID: sid}
				}
			}
			return m, nil
		}
		// Snippet picker
		if viewkeys.MatchAny(msg.String(), m.vk.SnippetPicker) {
			return m, func() tea.Msg { return types.SnippetPickerRequestMsg{} }
		}
		// Any keypress snaps back to live view
		m.scrollOffset = 0
		if b := m.encodeKey(msg); len(b) > 0 && m.sess != nil && m.sess.Stdin != nil {
			_, _ = m.sess.Stdin.Write(b)
		}
		return m, nil

	case tea.PasteMsg:
		if m.disconnected {
			return m, nil
		}
		if m.sess != nil && m.sess.Stdin != nil {
			_, _ = m.sess.Stdin.Write([]byte(msg.String()))
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.disconnected {
			return m, nil
		}
		// In alternate screen (vim, less), forward scroll to the remote app.
		if m.emu.IsAltScreen() {
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
		// Normal screen: scroll through scrollback history.
		maxScroll := m.emu.ScrollbackLen()
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollOffset += 3
			if m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
		case tea.MouseWheelDown:
			m.scrollOffset -= 3
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		}
		return m, nil
	}

	return m, nil
}
