package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

const (
	outputWindowBytes  = 256 * 1024
	outputRingBytes    = 2 * 1024 * 1024
	sendCtrlQueueSize  = 64
	sendDataQueueSize  = 64
	inputQueueSize     = 64
	sendRetryDelay     = 50 * time.Millisecond
	outputReadBufBytes = 8192
)

var errSenderClosed = errors.New("relay connection closed")

// outputRing is a fixed-capacity byte ring keyed by absolute stream offsets.
type outputRing struct {
	buf   []byte
	start int
	n     int
	base  uint64
}

func newOutputRing() *outputRing {
	return &outputRing{buf: make([]byte, outputRingBytes)}
}

func (r *outputRing) End() uint64 { return r.base + uint64(r.n) }

func (r *outputRing) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	if len(p) >= len(r.buf) {
		r.base = r.End() + uint64(len(p)-len(r.buf))
		copy(r.buf, p[len(p)-len(r.buf):])
		r.start = 0
		r.n = len(r.buf)
		return
	}
	end := (r.start + r.n) % len(r.buf)
	first := min(len(p), len(r.buf)-end)
	copy(r.buf[end:end+first], p[:first])
	copy(r.buf[:], p[first:])
	r.n += len(p)
	if r.n > len(r.buf) {
		over := r.n - len(r.buf)
		r.n = len(r.buf)
		r.start = (r.start + over) % len(r.buf)
		r.base += uint64(over)
	}
}

// ReadFrom copies up to max bytes starting at absolute offset off. Offsets
// older than the retained window are clamped to the oldest available byte.
func (r *outputRing) ReadFrom(off uint64, max int) []byte {
	if off < r.base {
		off = r.base
	}
	if off >= r.End() || max <= 0 {
		return nil
	}
	n := int(r.End() - off)
	if n > max {
		n = max
	}
	out := make([]byte, n)
	idx := (r.start + int(off-r.base)) % len(r.buf)
	first := min(n, len(r.buf)-idx)
	copy(out, r.buf[idx:idx+first])
	copy(out[first:], r.buf[:n-first])
	return out
}

// frameSender queues frames for one relay connection. Control frames drain
// before bulk FrameData output so heavy output never starves input/acks.
type frameSender struct {
	ctrl chan relay.Frame
	data chan relay.Frame
	done chan struct{}
}

func newFrameSender() *frameSender {
	return &frameSender{
		ctrl: make(chan relay.Frame, sendCtrlQueueSize),
		data: make(chan relay.Frame, sendDataQueueSize),
		done: make(chan struct{}),
	}
}

func (s *frameSender) send(f relay.Frame) error {
	ch := s.ctrl
	if f.Type == relay.FrameData {
		ch = s.data
	}
	select {
	case <-s.done:
		return errSenderClosed
	default:
	}
	select {
	case ch <- f:
		return nil
	case <-s.done:
		return errSenderClosed
	}
}

// drainData drops queued bulk frames. Called when a stream is resumed so
// stale pre-reattach output never reaches the client ahead of the replay.
func (s *frameSender) drainData() {
	for {
		select {
		case <-s.data:
		default:
			return
		}
	}
}

func (s *frameSender) run(ctx context.Context, c *websocket.Conn) {
	defer close(s.done)
	for {
		var f relay.Frame
		select {
		case f = <-s.ctrl:
		default:
			select {
			case f = <-s.ctrl:
			case f = <-s.data:
			case <-ctx.Done():
				return
			}
		}
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		err := c.Write(wctx, websocket.MessageBinary, relay.Encode(f))
		cancel()
		if err != nil {
			return
		}
	}
}

// streamRelay owns one PTY session's output pipeline. It outlives individual
// relay connections so a client can resume after a disconnect.
type streamRelay struct {
	is            *internalssh.InteractiveSession
	mu            sync.Mutex
	ring          *outputRing
	sent          uint64
	ack           uint64
	detachedSince time.Time
	sidV          atomic.Uint32 // current relay stream id; re-keyed when a named session is attached
	input         chan []byte
	wake          chan struct{}
	stop          chan struct{}
	stopOnce      sync.Once
}

func newStreamRelay(is *internalssh.InteractiveSession) *streamRelay {
	s := &streamRelay{
		is:    is,
		ring:  newOutputRing(),
		input: make(chan []byte, inputQueueSize),
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
	}
	go s.inputPump()
	return s
}

// queueInput enqueues client input without blocking the relay read loop; a
// stalled peer (XOFF, no reader) must not freeze every stream. When the queue
// is full the oldest frame is dropped.
func (s *streamRelay) queueInput(p []byte) {
	select {
	case s.input <- p:
	default:
		select {
		case <-s.input:
		case <-s.stop:
			return
		}
		select {
		case s.input <- p:
		case <-s.stop:
		}
	}
}

func (s *streamRelay) inputPump() {
	for {
		select {
		case p := <-s.input:
			if _, err := s.is.Stdin.Write(p); err != nil {
				return
			}
		case <-s.stop:
			return
		}
	}
}

func (s *streamRelay) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *streamRelay) shutdown() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *streamRelay) appendOutput(p []byte) {
	s.mu.Lock()
	s.ring.Write(p)
	s.mu.Unlock()
	s.notify()
}

func (s *streamRelay) setAck(ack uint64) {
	s.mu.Lock()
	// Clamp to sent: a stale ack from before a resume rewind must not push
	// ack past sent, which would underflow the inflight window.
	if ack > s.ack && ack <= s.sent {
		s.ack = ack
	}
	s.mu.Unlock()
	s.notify()
}

// markDetached notes that the client went away; an idle reaper destroys the
// stream if no attach follows within detachedStreamTTL.
func (s *streamRelay) markDetached() {
	s.mu.Lock()
	if s.detachedSince.IsZero() {
		s.detachedSince = time.Now()
	}
	s.mu.Unlock()
}

// attachForOpen atomically rewinds and queues OpenOK, so the client always
// sees OpenOK before any replayed data frame.
func (s *streamRelay) attachForOpen(fromSeq uint64, sender *frameSender, openOK relay.Frame) error {
	s.mu.Lock()
	if fromSeq < s.ring.base || fromSeq > s.ring.End() {
		s.mu.Unlock()
		return errors.New("resume offset outside retained buffer")
	}
	s.sent = fromSeq
	s.ack = fromSeq
	s.detachedSince = time.Time{}
	sender.drainData()
	err := sender.send(openOK)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// attachClamped rewinds like attachForOpen but clamps a stale offset up to
// the retained window instead of failing, and re-keys the stream onto a new
// relay stream id; used when a client attaches to a daemon-hosted session
// without knowing its resume offset.
func (s *streamRelay) attachClamped(streamID uint32, fromSeq uint64, sender *frameSender, openOK relay.Frame) error {
	s.mu.Lock()
	s.sidV.Store(streamID)
	if fromSeq < s.ring.base {
		fromSeq = s.ring.base
	}
	if fromSeq > s.ring.End() {
		s.mu.Unlock()
		return errors.New("resume offset outside retained buffer")
	}
	s.sent = fromSeq
	s.ack = fromSeq
	s.detachedSince = time.Time{}
	sender.drainData()
	err := sender.send(openOK)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

func (s *streamRelay) waitCredit() bool {
	for {
		s.mu.Lock()
		ok := s.ring.End()-s.ack < outputWindowBytes
		s.mu.Unlock()
		if ok {
			return true
		}
		select {
		case <-s.wake:
		case <-s.stop:
			return false
		}
	}
}

func (s *streamRelay) readPump(readDone chan<- error) {
	buf := make([]byte, outputReadBufBytes)
	for {
		if !s.waitCredit() {
			return
		}
		n, err := s.is.Stdout.Read(buf)
		if n > 0 {
			s.appendOutput(buf[:n])
		}
		if err != nil {
			readDone <- err
			return
		}
	}
}

func (s *streamRelay) pump(ctx context.Context, streamID uint32, mgr *sessionManager) {
	s.sidV.Store(streamID)
	readDone := make(chan error, 1)
	go s.readPump(readDone)
	var endErr error
	ended := false
	for {
		sender := mgr.sender()
		s.mu.Lock()
		sid := s.sidV.Load()
		sent, end, inflight := s.sent, s.ring.End(), s.sent-s.ack
		if sender != nil && sent < end && inflight < outputWindowBytes {
			chunk := s.ring.ReadFrom(sent, maxOutputFrameBytes)
			frame := relay.Frame{Type: relay.FrameData, StreamID: sid, Payload: relay.DataPayload(sent, chunk)}
			// Send while holding mu: attachForOpen rewinds and drains under
			// the same lock, so a pre-rewind frame can never slip past the
			// drain and reach the client ahead of the replay.
			err := sender.send(frame)
			if err == nil {
				s.sent += uint64(len(chunk))
			}
			s.mu.Unlock()
			if err != nil {
				select {
				case <-time.After(sendRetryDelay):
				case <-s.stop:
					return
				case <-ctx.Done():
					return
				}
			}
			continue
		}
		s.mu.Unlock()
		if ended && sender != nil && sent >= end {
			// Remove by identity: an attach may have re-keyed the stream onto
			// another id, and a named session entry bound to it must go too,
			// otherwise list/attach keep showing a dead session.
			if closeID, ok := mgr.removeStream(s); ok {
				s.shutdown()
				_ = s.is.Close()
				_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: closeID, Payload: closePayload(endErr)})
			}
			return
		}
		// An ended stream with no connection stays registered: the ring keeps
		// the tail for a later resume; the idle reaper destroys it otherwise.
		select {
		case err := <-readDone:
			endErr = sessionDoneErr(err, s.is.Done)
			ended = true
		case err := <-s.is.Done:
			endErr = err
			ended = true
		case <-s.wake:
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

type sessionManager struct {
	mu       sync.Mutex
	streams  map[uint32]*streamRelay
	named    map[string]*namedSession
	attachMu sync.Mutex // serializes named-session attach against itself and kill
	senderV  atomic.Pointer[frameSender]
}

const (
	detachedStreamTTL = 10 * time.Minute
	reapCheckInterval = time.Minute
)

func newSessionManager() *sessionManager {
	return &sessionManager{streams: make(map[uint32]*streamRelay), named: make(map[string]*namedSession)}
}

func (m *sessionManager) sender() *frameSender { return m.senderV.Load() }

func (m *sessionManager) setSender(s *frameSender) { m.senderV.Store(s) }

func (m *sessionManager) clearSender(s *frameSender) {
	if !m.senderV.CompareAndSwap(s, nil) {
		return
	}
	// Connection dropped: stamp every stream so the reaper can expire it even
	// if the daemon reconnects before the next reap tick and no client resumes.
	m.mu.Lock()
	for _, sr := range m.streams {
		sr.markDetached()
	}
	m.mu.Unlock()
}

func (m *sessionManager) get(streamID uint32) *streamRelay {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[streamID]
}

func (m *sessionManager) add(streamID uint32, s *streamRelay) {
	m.mu.Lock()
	m.streams[streamID] = s
	m.mu.Unlock()
}

func (m *sessionManager) remove(streamID uint32, expected *streamRelay) *streamRelay {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.streams[streamID]
	if s == nil || (expected != nil && s != expected) {
		return nil
	}
	delete(m.streams, streamID)
	return s
}

// removeStream deletes whichever stream id currently maps to s (an attach may
// have re-keyed it) along with any named session entry bound to that id.
func (m *sessionManager) removeStream(s *streamRelay) (uint32, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sr := range m.streams {
		if sr != s {
			continue
		}
		delete(m.streams, id)
		for name, ns := range m.named {
			if ns.streamID == id {
				delete(m.named, name)
			}
		}
		return id, true
	}
	return 0, false
}

// reapLoop destroys streams whose client never came back.
func (m *sessionManager) reapLoop(ctx context.Context) {
	t := time.NewTicker(reapCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reapDetached(time.Now())
		}
	}
}

func (m *sessionManager) reapDetached(now time.Time) {
	m.mu.Lock()
	allDetached := m.sender() == nil
	persistent := make(map[uint32]bool, len(m.named))
	for _, ns := range m.named {
		persistent[ns.streamID] = true
	}
	var expiredIDs []uint32
	var expired []*streamRelay
	for id, sr := range m.streams {
		if persistent[id] {
			continue
		}
		sr.mu.Lock()
		if allDetached && sr.detachedSince.IsZero() {
			sr.detachedSince = now
		}
		stale := !sr.detachedSince.IsZero() && now.Sub(sr.detachedSince) >= detachedStreamTTL
		sr.mu.Unlock()
		if stale {
			expiredIDs = append(expiredIDs, id)
			expired = append(expired, sr)
		}
	}
	m.mu.Unlock()
	for i, sr := range expired {
		if m.remove(expiredIDs[i], sr) != nil {
			sr.shutdown()
			_ = sr.is.Close()
		}
	}
}

func (m *sessionManager) closeAll() {
	m.mu.Lock()
	open := make([]*streamRelay, 0, len(m.streams))
	for id, s := range m.streams {
		open = append(open, s)
		delete(m.streams, id)
	}
	m.mu.Unlock()
	for _, s := range open {
		s.shutdown()
		_ = s.is.Close()
	}
}
