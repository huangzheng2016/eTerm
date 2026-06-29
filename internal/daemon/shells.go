package daemon

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

var errShellNotFound = errors.New("active shell not found")

type activeShell struct {
	id      string
	shell   string
	name    string
	created time.Time
	is      *internalssh.InteractiveSession

	mu       sync.Mutex
	ring     *ringBuffer
	stream   uint32
	write    func(relay.Frame) error
	stopPump chan struct{}
	onExit   func(*activeShell)
}

func (s *activeShell) detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stream = 0
	s.write = nil
}

func (s *activeShell) pump() {
	defer func() {
		if s.onExit != nil {
			s.onExit(s)
		}
	}()
	buf := make([]byte, 8192)
	for {
		n, err := s.is.Stdout.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.ring.Write(buf[:n])
			if s.write != nil {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				_ = s.write(relay.Frame{Type: relay.FrameData, StreamID: s.stream, Payload: payload})
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
		select {
		case <-s.stopPump:
			return
		default:
		}
	}
}

type shellManager struct {
	mu         sync.Mutex
	shells     map[string]*activeShell
	newSession func(rows, cols int) (*internalssh.InteractiveSession, error)
	shellName  func() string
}

func newShellManager(newSession func(rows, cols int) (*internalssh.InteractiveSession, error), shellName func() string) *shellManager {
	return &shellManager{
		shells:     map[string]*activeShell{},
		newSession: newSession,
		shellName:  shellName,
	}
}

func (m *shellManager) list() []relay.ActiveShellInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]relay.ActiveShellInfo, 0, len(m.shells))
	for _, s := range m.shells {
		s.mu.Lock()
		busy := s.stream != 0
		name := s.name
		s.mu.Unlock()
		out = append(out, relay.ActiveShellInfo{
			ID:          s.id,
			Shell:       s.shell,
			Name:        name,
			CreatedUnix: s.created.Unix(),
			Busy:        busy,
		})
	}
	return out
}

func (m *shellManager) create(stream uint32, rows, cols int, write func(relay.Frame) error) (*activeShell, error) {
	is, err := m.newSession(rows, cols)
	if err != nil {
		return nil, err
	}
	s := &activeShell{
		id:       uuid.New().String()[:6],
		shell:    m.shellName(),
		created:  time.Now(),
		is:       is,
		ring:     newRingBuffer(),
		stream:   stream,
		write:    write,
		stopPump: make(chan struct{}),
		onExit:   m.removeExited,
	}
	m.mu.Lock()
	m.shells[s.id] = s
	m.mu.Unlock()
	return s, nil
}

func (s *activeShell) start() {
	go s.pump()
}

func (m *shellManager) get(id string) *activeShell {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shells[id]
}

func (m *shellManager) attach(id string, stream uint32, rows, cols int, write func(relay.Frame) error) (*activeShell, uint32, []byte, error) {
	s := m.get(id)
	if s == nil {
		return nil, 0, nil, errShellNotFound
	}
	s.mu.Lock()
	var displaced uint32
	if s.stream != 0 && s.write != nil {
		displaced = s.stream
		_ = s.write(relay.Frame{Type: relay.FrameClose, StreamID: s.stream})
	}
	s.stream = stream
	s.write = write
	replay := s.ring.Bytes()
	s.mu.Unlock()
	if s.is.Resize != nil {
		// Force a SIGWINCH even when the size is unchanged so full-screen
		// programs and the shell prompt repaint immediately after re-attach.
		if rows > 1 {
			_ = s.is.Resize(rows-1, cols)
		}
		_ = s.is.Resize(rows, cols)
	}
	return s, displaced, replay, nil
}

func (m *shellManager) kill(id string) {
	m.mu.Lock()
	s := m.shells[id]
	delete(m.shells, id)
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	stream := s.stream
	write := s.write
	s.stream = 0
	s.write = nil
	s.mu.Unlock()
	if stream != 0 && write != nil {
		_ = write(relay.Frame{Type: relay.FrameClose, StreamID: stream})
	}
	close(s.stopPump)
	_ = s.is.Close()
}

func (m *shellManager) rename(id, name string) error {
	m.mu.Lock()
	s := m.shells[id]
	m.mu.Unlock()
	if s == nil {
		return errShellNotFound
	}
	s.mu.Lock()
	s.name = strings.TrimSpace(name)
	s.mu.Unlock()
	return nil
}

func (m *shellManager) removeExited(s *activeShell) {
	m.mu.Lock()
	if m.shells[s.id] != s {
		m.mu.Unlock()
		return
	}
	delete(m.shells, s.id)
	m.mu.Unlock()

	s.mu.Lock()
	stream := s.stream
	write := s.write
	s.stream = 0
	s.write = nil
	s.mu.Unlock()
	if stream != 0 && write != nil {
		_ = write(relay.Frame{Type: relay.FrameClose, StreamID: stream})
	}
	_ = s.is.Close()
}

func (m *shellManager) detachStream(stream uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.shells {
		s.mu.Lock()
		if s.stream == stream {
			s.stream = 0
			s.write = nil
		}
		s.mu.Unlock()
	}
}

func (m *shellManager) detachAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.shells {
		s.mu.Lock()
		s.stream = 0
		s.write = nil
		s.mu.Unlock()
	}
}
