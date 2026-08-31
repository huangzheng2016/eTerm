// Package sshview renders an SSH shell inside Bubble Tea (no tea.Exec), so the tab bar stays visible.
// Terminal emulation uses github.com/charmbracelet/x/vt (ANSI screen state, cursor, colors).
//
// Full-screen TUI programs (vim, less, more) run in the remote shell; they use the PTY size
// from SetSize and TERM from the session. They are not implemented in Go here—standard
// OpenSSH-style behaviour once the pty geometry matches the layout row count.
package sshview

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
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

const selectionAutoScrollEdgePercent = 20

const maxCoalescedChunkBytes = 64 * 1024

const outputCoalesceInterval = 8 * time.Millisecond

const inputQueueSize = 512

const resizeQueueSize = 4

const scrollIndicatorDuration = 3 * time.Second

const remoteTmuxReconnectAttempts = 3

var xtgettcapValues = map[string]string{
	"Tc":     "1",
	"RGB":    "8/8/8",
	"Ms":     "\x1b]52;%p1%s;%p2%s\a",
	"TN":     "xterm-256color",
	"Co":     "256",
	"colors": "256",
}

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

type scrollIndicatorTimeoutMsg struct {
	StreamID uint64
	Seq      uint64
}

type resizeRequest struct {
	rows int
	cols int
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

	// remote, when set, reconnects this session over the relay (hostID is 0).
	remote *types.RemoteReconnect

	ch     chan []byte
	mu     sync.Mutex
	closed bool

	inputMu     sync.Mutex
	inputCh     chan []byte
	inputClosed bool

	resizeMu     sync.Mutex
	resizeCh     chan resizeRequest
	resizeClosed bool

	endErr       error
	waitComplete bool
	doneClosed   chan struct{}
	disconnected bool
	reconnecting bool
	reconnectTry int
	reconnectMax int

	// Scrollback view: scrollOffset > 0 means viewing history.
	// 0 = live view (bottom of scrollback), N = N lines scrolled up.
	scrollOffset int

	// bottomPad > 0 lets the user scroll past the live bottom, pushing the
	// newest line up and showing empty rows below it (0..bottomPadMax).
	bottomPad int

	scrollIndicatorUntil time.Time
	scrollIndicatorSeq   uint64

	// Mouse drag text selection over the visible screen + scrollback.
	sel selection

	selectionAutoScrollDir    int
	selectionAutoScrollQueued bool

	// Configurable keybindings
	vk viewkeys.SSHKeys

	appCursorKeys  bool
	mouseMode      bool
	bracketedPaste bool
	cursorHidden   bool

	osc52Clipboard    []string
	osc9Notifications []string
	recorder          *Recorder

	// Command lifecycle from OSC 133 (set from emulator callbacks during Write).
	cmdRunning   bool
	cmdCount     int
	lastExitCode int
	hasExitCode  bool
}

func (m *Model) SetViewKeys(vk viewkeys.SSHKeys) { m.vk = vk }

// SetRemoteReconnect marks this session as relay-backed so "r" reconnects over
// the relay instead of redialing a DB host.
func (m *Model) SetRemoteReconnect(r *types.RemoteReconnect) { m.remote = r }

func (m *Model) RemoteReconnect() *types.RemoteReconnect {
	if m.remote == nil {
		return nil
	}
	r := *m.remote
	return &r
}

// New creates a model; call SetSize or rely on WindowSizeMsg. hostID is used to reconnect after a network drop.
func New(is *internalssh.InteractiveSession, alias string, hostID uint, vk viewkeys.SSHKeys) *Model {
	emu := vt.NewEmulator(80, 24)
	// tmux asks these xterm probes before repainting full-screen panes.
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'n'), func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 0)
		if !ok || n != 996 {
			return false
		}
		_, _ = io.WriteString(emu.InputPipe(), ansi.LightDarkReport(true))
		return true
	})
	emu.RegisterCsiHandler(ansi.Command('>', 0, 'q'), func(params ansi.Params) bool {
		_, _ = io.WriteString(emu.InputPipe(), "\x1bP>|eTerm\x1b\\")
		return true
	})
	emu.RegisterCsiHandler('t', func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 0)
		if !ok || n != 18 {
			return false
		}
		_, _ = io.WriteString(emu.InputPipe(), ansi.WindowOp(8, emu.Height(), emu.Width()))
		return true
	})
	emu.RegisterDcsHandler(ansi.Command(0, '+', 'q'), func(_ ansi.Params, data []byte) bool {
		_, _ = io.WriteString(emu.InputPipe(), xtgettcapReply(data))
		return true
	})
	m := &Model{
		sess:       is,
		emu:        emu,
		streamID:   streamIDGen.Add(1),
		alias:      alias,
		hostID:     hostID,
		ch:         make(chan []byte, 128),
		inputCh:    make(chan []byte, inputQueueSize),
		resizeCh:   make(chan resizeRequest, resizeQueueSize),
		doneClosed: make(chan struct{}),
		vk:         vk,
	}
	m.startInputWriter()
	m.startResizeWriter()
	emu.RegisterOscHandler(52, func(data []byte) bool {
		if text, ok := osc52ClipboardText(data); ok {
			m.osc52Clipboard = append(m.osc52Clipboard, text)
		}
		return true
	})
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
		CursorVisibility: func(visible bool) {
			m.cursorHidden = !visible
		},
		Notification: func(text string) {
			m.osc9Notifications = append(m.osc9Notifications, text)
		},
		CommandStart: func() {
			m.cmdRunning = true
		},
		CommandEnd: func(code int) {
			m.cmdRunning = false
			m.cmdCount++
			m.lastExitCode = code
			m.hasExitCode = true
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
			if n > 0 {
				if sess := m.currentSession(); sess != nil && sess.Stdin != nil {
					m.queueInput(buf[:n])
				}
			}
		}
	}()
	return m
}

func (m *Model) startInputWriter() {
	ch := m.inputCh
	go func() {
		for b := range ch {
			if sess := m.currentSession(); sess != nil && sess.Stdin != nil {
				_, _ = sess.Stdin.Write(b)
			}
		}
	}()
}

func (m *Model) startResizeWriter() {
	ch := m.resizeCh
	go func() {
		for req := range ch {
			if sess := m.currentSession(); sess != nil && sess.Resize != nil {
				_ = sess.Resize(req.rows, req.cols)
			}
		}
	}()
}

func (m *Model) currentSession() *internalssh.InteractiveSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sess
}

func (m *Model) queueInput(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	b := append([]byte(nil), p...)
	m.inputMu.Lock()
	defer m.inputMu.Unlock()
	if m.inputClosed || m.inputCh == nil {
		return false
	}
	select {
	case m.inputCh <- b:
		if m.recorder != nil {
			m.recorder.Input(b)
		}
		return true
	default:
		return false
	}
}

func (m *Model) queueResize(rows, cols int) bool {
	m.resizeMu.Lock()
	defer m.resizeMu.Unlock()
	if m.resizeClosed || m.resizeCh == nil {
		return false
	}
	req := resizeRequest{rows: rows, cols: cols}
	select {
	case m.resizeCh <- req:
		return true
	default:
		select {
		case <-m.resizeCh:
		default:
		}
		select {
		case m.resizeCh <- req:
			return true
		default:
			return false
		}
	}
}

func xtgettcapReply(data []byte) string {
	var b strings.Builder
	for _, raw := range strings.Split(string(data), ";") {
		if raw == "" {
			continue
		}
		capHex := strings.ToUpper(raw)
		decoded, err := hex.DecodeString(raw)
		value, ok := xtgettcapValues[string(decoded)]
		if err != nil || !ok {
			b.WriteString("\x1bP0+r")
			b.WriteString(capHex)
			b.WriteString("\x1b\\")
			continue
		}
		b.WriteString("\x1bP1+r")
		b.WriteString(capHex)
		b.WriteByte('=')
		b.WriteString(strings.ToUpper(hex.EncodeToString([]byte(value))))
		b.WriteString("\x1b\\")
	}
	if b.Len() == 0 {
		return "\x1bP0+r\x1b\\"
	}
	return b.String()
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

func (m *Model) EnableReplayRecording() {
	if m.recorder == nil {
		m.recorder = NewRecorder(time.Now())
		m.recorder.Resize(m.emu.Height(), m.emu.Width())
	}
}

func (m *Model) ReplayRecording() ([]byte, time.Duration, bool) {
	if m == nil || m.recorder == nil {
		return nil, 0, false
	}
	return m.recorder.Close()
}

func (m *Model) ReplayRecordingEnabled() bool {
	return m != nil && m.recorder != nil
}

// LastCommandExitCode returns the exit code of the last command reported via
// OSC 133;D (-1 when the shell omitted it). ok is false before the first
// command finishes.
func (m *Model) LastCommandExitCode() (code int, ok bool) {
	return m.lastExitCode, m.hasExitCode
}

// CommandCount returns how many commands have finished (OSC 133;D count).
func (m *Model) CommandCount() int { return m.cmdCount }

// CommandRunning reports whether a command is currently executing (between
// OSC 133;C and OSC 133;D).
func (m *Model) CommandRunning() bool { return m.cmdRunning }

// PasteCommand writes a command string to the SSH session stdin.
func (m *Model) PasteCommand(cmd string) {
	if m.disconnected || m.sess == nil || m.sess.Stdin == nil {
		return
	}
	m.queueInput([]byte(cmd))
}

func (m *Model) PasteText(text string) {
	m.PasteCommand(text)
}

// SendRaw writes raw bytes to the session stdin through the same queue as
// typed keys (no bracketed-paste wrapping, so control bytes like \x03 reach
// the pty as-is). Returns false when the session is not writable.
func (m *Model) SendRaw(s string) bool {
	if m.disconnected || m.sess == nil || m.sess.Stdin == nil {
		return false
	}
	return m.queueInput([]byte(s))
}

// Session returns the current session so the app layer can extract relay
// resume state (stream id and consumed offset) after a disconnect.
func (m *Model) Session() *internalssh.InteractiveSession { return m.currentSession() }

// ResumeSession swaps in a resumed relay session, keeping the emulator and
// its scrollback, and restarts the read plumbing. The replayed output is
// re-fed to the emulator, restoring the visible state.
func (m *Model) ResumeSession(is *internalssh.InteractiveSession) tea.Cmd {
	m.mu.Lock()
	oldCh := m.ch
	m.sess = is
	m.endErr = nil
	m.waitComplete = false
	m.closed = false
	m.ch = make(chan []byte, 128)
	m.doneClosed = make(chan struct{})
	m.mu.Unlock()
	// Render output that was acked but still queued in the old channel, so
	// resuming from nextSeq loses nothing.
drain:
	for {
		select {
		case b, ok := <-oldCh:
			if !ok {
				break drain
			}
			if m.recorder != nil {
				m.recorder.Output(b)
			}
			m.writeEmulator(b)
		default:
			break drain
		}
	}
	m.disconnected = false
	m.reconnecting = false
	m.reconnectTry = 0
	m.reconnectMax = 0
	if m.width > 0 {
		m.SetSize(m.width, m.height)
	}
	go m.readLoop()
	go m.watchDone()
	return waitChunk(m)
}

// Disconnected is true after a network-style drop; press "r" to send [types.SSHReconnectMsg].
func (m *Model) Disconnected() bool { return m.disconnected }

func (m *Model) SetDisconnected(err error) {
	m.endErr = err
	m.disconnected = true
}

func (m *Model) SetReconnecting(attempt, maxAttempts int) {
	if attempt <= 0 || maxAttempts <= 0 {
		m.reconnecting = false
		m.reconnectTry = 0
		m.reconnectMax = 0
		return
	}
	m.reconnecting = true
	m.reconnectTry = attempt
	m.reconnectMax = maxAttempts
}

func (m *Model) ReconnectingLabel() string {
	if !m.reconnecting {
		return ""
	}
	return fmt.Sprintf("RECONNECTING (%d/%d)", m.reconnectTry, m.reconnectMax)
}

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
	if m.recorder != nil {
		m.recorder.Resize(termH, w)
	}
	if m.sess != nil && m.sess.Resize != nil {
		m.queueResize(termH, w)
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
	go m.watchDone()
	return waitChunk(m)
}

// watchDone waits for the current session to end. Init and ResumeSession each
// start one; it captures the session and done channel at start and only
// records the error if that session is still current.
func (m *Model) watchDone() {
	m.mu.Lock()
	sess := m.sess
	doneClosed := m.doneClosed
	m.mu.Unlock()
	if sess == nil {
		return
	}
	err := <-sess.Done
	m.mu.Lock()
	current := m.sess == sess
	m.mu.Unlock()
	if current {
		m.setWaitErr(err)
	}
	if doneClosed != nil {
		close(doneClosed)
	}
}

func (m *Model) readLoop() {
	m.mu.Lock()
	sess := m.sess
	ch := m.ch
	m.mu.Unlock()
	if sess == nil || sess.Stdout == nil {
		m.closeChFor(ch)
		return
	}
	buf := make([]byte, 8192)
	for {
		n, err := sess.Stdout.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			ch <- b
		}
		if err != nil {
			m.mu.Lock()
			current := m.sess == sess
			m.mu.Unlock()
			if current {
				m.setReadErr(err)
			}
			m.closeChFor(ch)
			return
		}
	}
}

// closeChFor closes ch only if it is still the live chunk channel; a stale
// readLoop from before a ResumeSession must not close the new channel.
func (m *Model) closeChFor(ch chan []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ch != ch {
		return
	}
	m.closed = true
	close(m.ch)
}

func waitChunk(m *Model) tea.Cmd {
	// Capture the generation's channels under lock: ResumeSession replaces
	// them, and an in-flight wait on a superseded channel must not leak or
	// report a stale StreamDoneMsg.
	m.mu.Lock()
	ch := m.ch
	doneClosed := m.doneClosed
	m.mu.Unlock()
	return func() tea.Msg {
		b, ok := <-ch
		if !ok {
			m.mu.Lock()
			if m.ch != ch {
				m.mu.Unlock()
				return nil
			}
			m.mu.Unlock()
			if doneClosed != nil {
				select {
				case <-doneClosed:
				case <-time.After(250 * time.Millisecond):
				}
			}
			m.mu.Lock()
			err := m.endErr
			m.mu.Unlock()
			return StreamDoneMsg{StreamID: m.streamID, Err: err}
		}
		data := coalesceQueuedChunks(ch, b)
		if m.recorder != nil {
			m.recorder.Output(data)
		}
		return ChunkMsg{StreamID: m.streamID, Data: data}
	}
}

func coalesceQueuedChunks(ch <-chan []byte, first []byte) []byte {
	if len(first) >= maxCoalescedChunkBytes {
		return first
	}
	out := first
	timer := time.NewTimer(outputCoalesceInterval)
	defer timer.Stop()
	for len(out) < maxCoalescedChunkBytes {
		select {
		case next, ok := <-ch:
			if !ok {
				return out
			}
			if len(next) == 0 {
				continue
			}
			if len(out)+len(next) > maxCoalescedChunkBytes {
				out = append(out, next...)
				return out
			}
			out = append(out, next...)
		case <-timer.C:
			return out
		}
	}
	return out
}

// Close ends the SSH session.
func (m *Model) Close() error {
	// Close the emulator's input pipe to unblock the drain goroutine. We avoid
	// emu.Close() here because it writes an unsynchronized internal flag that the
	// drain goroutine reads via emu.Read, which the race detector flags.
	if c, ok := m.emu.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
	m.closeInputQueue()
	m.closeResizeQueue()
	if m.recorder != nil {
		// finalizeSSHSession already harvested the data when a history row
		// exists; this only cleans up otherwise (temp file fd included).
		m.recorder.Discard()
	}
	if m.sess != nil {
		return m.sess.Close()
	}
	return nil
}

func (m *Model) closeInputQueue() {
	m.inputMu.Lock()
	defer m.inputMu.Unlock()
	if m.inputClosed {
		return
	}
	m.inputClosed = true
	if m.inputCh != nil {
		close(m.inputCh)
	}
}

func (m *Model) closeResizeQueue() {
	m.resizeMu.Lock()
	defer m.resizeMu.Unlock()
	if m.resizeClosed {
		return
	}
	m.resizeClosed = true
	if m.resizeCh != nil {
		close(m.resizeCh)
	}
}

func (m *Model) writeEmulator(data []byte) {
	defer func() {
		if recover() == nil {
			return
		}
		w, h := m.width, m.height
		if w < 1 {
			w = m.emu.Width()
		}
		if h < 1 {
			h = m.emu.Height()
		}
		m.emu.Resize(w, h)
	}()
	_, _ = m.emu.Write(data)
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
		m.writeEmulator(msg.Data)
		clipCmds := m.takeOSC52ClipboardCommands()
		notifyCmds := m.takeOSC9NotificationCommands()
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
		return m, tea.Batch(append(append(clipCmds, notifyCmds...), waitChunk(m))...)

	case StreamDoneMsg:
		if msg.StreamID != m.streamID {
			return m, nil
		}
		err := msg.Err
		if shouldOfferReconnect(err) {
			m.disconnected = true
			if m.sess != nil {
				// Keep the dead session around: the app layer reads relay
				// resume state from it before opening the replacement.
				_ = m.sess.Close()
			}
			if m.remote != nil && m.remote.Tmux {
				m.SetReconnecting(1, remoteTmuxReconnectAttempts)
				spec := *m.remote
				return m, func() tea.Msg {
					return types.RemoteShellReconnectMsg{StreamID: m.streamID, Spec: spec, Auto: true, Attempt: 1, MaxAttempts: remoteTmuxReconnectAttempts}
				}
			}
			alias := m.alias
			retry := tea.Msg(nil)
			if m.remote != nil {
				retry = types.RemoteShellReconnectMsg{StreamID: m.streamID, Spec: *m.remote}
			} else if m.hostID != 0 {
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
			if viewkeys.MatchKey(msg, m.vk.Reconnect) {
				sid := m.streamID
				if m.remote != nil {
					spec := *m.remote
					return m, func() tea.Msg {
						return types.RemoteShellReconnectMsg{StreamID: sid, Spec: spec}
					}
				}
				if m.hostID != 0 {
					hid := m.hostID
					return m, func() tea.Msg {
						return types.SSHReconnectMsg{HostID: hid, StreamID: sid}
					}
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
		m.clearScrollIndicator()
		m.sel.active = false
		if b := m.encodeKey(msg); len(b) > 0 && m.sess != nil && m.sess.Stdin != nil {
			m.queueInput(b)
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
			m.queueInput(payload)
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
		m.sel.moved = true
		return m, m.updateSelectionAutoScroll(y)

	case tea.MouseReleaseMsg:
		if m.emu.IsAltScreen() && m.sendRemoteMouse(msg) {
			return m, nil
		}
		if !m.sel.dragging {
			return m, nil
		}
		w, h := m.emu.Width(), m.emu.Height()
		inside := msg.X >= 0 && msg.Y >= 0 && (w <= 0 || msg.X < w) && (h <= 0 || msg.Y < h)
		if inside {
			x, y := m.clampMouse(msg.X, msg.Y)
			m.sel.caret = selPoint{line: m.visibleAbsLine(y), col: x}
		}
		m.sel.dragging = false
		m.selectionAutoScrollDir = 0
		m.selectionAutoScrollQueued = false
		// Click without drag clears the selection; a real drag copies.
		if m.sel.anchor == m.sel.caret && !m.sel.moved {
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
						m.queueInput(seq)
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
		if m.scrollOffset > 0 {
			return m, m.showScrollIndicator()
		}
		m.clearScrollIndicator()
		return m, nil

	case scrollIndicatorTimeoutMsg:
		if msg.StreamID != m.streamID || msg.Seq != m.scrollIndicatorSeq {
			return m, nil
		}
		if !m.scrollIndicatorUntil.IsZero() && !time.Now().Before(m.scrollIndicatorUntil) {
			m.clearScrollIndicator()
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
		m.sel.moved = true
		return m, m.queueSelectionAutoScroll()
	}

	return m, nil
}

func osc52ClipboardText(data []byte) (string, bool) {
	parts := strings.SplitN(string(data), ";", 3)
	if len(parts) != 3 || parts[0] != "52" || parts[2] == "?" {
		return "", false
	}
	if parts[1] != "" && !strings.Contains(parts[1], "c") {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

func (m *Model) takeOSC52ClipboardCommands() []tea.Cmd {
	if len(m.osc52Clipboard) == 0 {
		return nil
	}
	out := make([]tea.Cmd, 0, len(m.osc52Clipboard))
	for _, text := range m.osc52Clipboard {
		out = append(out, tea.SetClipboard(text))
	}
	m.osc52Clipboard = nil
	return out
}

// osc9Sequence re-emits an OSC 9 notification to the outer terminal with
// control characters stripped so the payload cannot inject sequences.
func osc9Sequence(text string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
	return "\x1b]9;" + clean + "\a"
}

func (m *Model) takeOSC9NotificationCommands() []tea.Cmd {
	if len(m.osc9Notifications) == 0 {
		return nil
	}
	out := make([]tea.Cmd, 0, len(m.osc9Notifications))
	for _, text := range m.osc9Notifications {
		out = append(out, tea.Raw(osc9Sequence(text)))
	}
	m.osc9Notifications = nil
	return out
}

func (m *Model) showScrollIndicator() tea.Cmd {
	m.scrollIndicatorSeq++
	m.scrollIndicatorUntil = time.Now().Add(scrollIndicatorDuration)
	streamID := m.streamID
	seq := m.scrollIndicatorSeq
	return tea.Tick(scrollIndicatorDuration, func(time.Time) tea.Msg {
		return scrollIndicatorTimeoutMsg{StreamID: streamID, Seq: seq}
	})
}

func (m *Model) clearScrollIndicator() {
	m.scrollIndicatorUntil = time.Time{}
}

func (m *Model) scrollIndicatorVisible(now time.Time) bool {
	return m.scrollOffset > 0 && !m.scrollIndicatorUntil.IsZero() && now.Before(m.scrollIndicatorUntil)
}

func (m *Model) updateSelectionAutoScroll(y int) tea.Cmd {
	h := m.emu.Height()
	if h <= 0 {
		return nil
	}
	edgeRows := max(2, h/selectionAutoScrollEdgePercent)
	switch {
	case y < edgeRows:
		m.selectionAutoScrollDir = -1
	case y >= h-edgeRows:
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
