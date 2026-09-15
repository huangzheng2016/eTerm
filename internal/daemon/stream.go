package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

const (
	outputWindowBytes  = 1024 * 1024
	outputRingBytes    = 2 * 1024 * 1024
	sendCtrlQueueSize  = 64
	sendDataQueueSize  = 32
	inputQueueSize     = 64
	sendRetryDelay     = 50 * time.Millisecond
	outputReadBufBytes = 32 * 1024
)

var errSenderClosed = errors.New("relay connection closed")

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

func (r *outputRing) ReadInto(off uint64, dst []byte) int {
	if off < r.base {
		off = r.base
	}
	if off >= r.End() || len(dst) == 0 {
		return 0
	}
	n := int(r.End() - off)
	if n > len(dst) {
		n = len(dst)
	}
	idx := (r.start + int(off-r.base)) % len(r.buf)
	first := min(n, len(r.buf)-idx)
	copy(dst, r.buf[idx:idx+first])
	copy(dst[first:], r.buf[:n-first])
	return n
}

type frameSender struct {
	ctrl chan relay.Frame
	data chan []byte
	done chan struct{}
}

func newFrameSender() *frameSender {
	return &frameSender{
		ctrl: make(chan relay.Frame, sendCtrlQueueSize),
		data: make(chan []byte, sendDataQueueSize),
		done: make(chan struct{}),
	}
}

func (s *frameSender) send(f relay.Frame) error {
	select {
	case <-s.done:
		return errSenderClosed
	default:
	}
	select {
	case s.ctrl <- f:
		return nil
	case <-s.done:
		return errSenderClosed
	}
}

func (s *frameSender) sendData(b []byte) error {
	select {
	case <-s.done:
		return errSenderClosed
	default:
	}
	select {
	case s.data <- b:
		return nil
	case <-s.done:
		return errSenderClosed
	}
}

func (s *frameSender) drainData(streamID uint32) {
	var keep [][]byte
	for {
		select {
		case b := <-s.data:
			if relay.PeekStreamID(b) != streamID {
				keep = append(keep, b)
			}
		default:
			for _, b := range keep {
				_ = s.sendData(b)
			}
			return
		}
	}
}

func recoverLog(label func() string) {
	if r := recover(); r != nil {
		log.Printf("eterm daemon %s panic: %v\n%s", label(), r, debug.Stack())
	}
}

func (s *frameSender) run(ctx context.Context, c *websocket.Conn) {
	defer close(s.done)
	defer recoverLog(func() string { return "frame sender" })
	for {
		var msg []byte
		select {
		case f := <-s.ctrl:
			msg = relay.Encode(f)
		default:
			select {
			case f := <-s.ctrl:
				msg = relay.Encode(f)
			case msg = <-s.data:
			case <-ctx.Done():
				return
			}
		}
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		err := c.Write(wctx, websocket.MessageBinary, msg)
		cancel()
		if err != nil {
			return
		}
	}
}

type streamRelay struct {
	is            *internalssh.InteractiveSession
	mu            sync.Mutex
	ring          *outputRing
	sent          uint64
	ack           uint64
	gen           uint64
	detachedSince time.Time
	sidV          atomic.Uint32
	input         chan []byte
	wake          chan struct{}
	stop          chan struct{}
	stopOnce      sync.Once
	stallAfter    time.Duration
	stallInterval time.Duration
}

func newStreamRelay(is *internalssh.InteractiveSession) *streamRelay {
	s := &streamRelay{
		is:            is,
		ring:          newOutputRing(),
		input:         make(chan []byte, inputQueueSize),
		wake:          make(chan struct{}, 1),
		stop:          make(chan struct{}),
		stallAfter:    stallLogAfter,
		stallInterval: stallLogInterval,
	}
	go s.inputPump()
	return s
}

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
	defer recoverLog(func() string { return fmt.Sprintf("stream %d input pump", s.sidV.Load()) })
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
	if ack > s.ack && ack <= s.sent {
		s.ack = ack
	}
	s.mu.Unlock()
	s.notify()
}

func (s *streamRelay) markDetached() {
	s.mu.Lock()
	if s.detachedSince.IsZero() {
		s.detachedSince = time.Now()
	}
	s.mu.Unlock()
}

func (s *streamRelay) attachForOpen(fromSeq uint64, sender *frameSender, openOK relay.Frame) error {
	s.mu.Lock()
	if fromSeq < s.ring.base || fromSeq > s.ring.End() {
		s.mu.Unlock()
		return errors.New("resume offset outside retained buffer")
	}
	s.sent = fromSeq
	s.ack = fromSeq
	s.gen++
	s.detachedSince = time.Time{}
	sender.drainData(openOK.StreamID)
	err := sender.send(openOK)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

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
	s.gen++
	s.detachedSince = time.Time{}
	sender.drainData(streamID)
	err := sender.send(openOK)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

func (s *streamRelay) drainIfGenChanged(gen uint64, sid uint32, sender *frameSender) {
	s.mu.Lock()
	if s.gen != gen {
		sender.drainData(sid)
	}
	s.mu.Unlock()
}

const (
	stallLogAfter    = 60 * time.Second
	stallLogInterval = 60 * time.Second
)

func (s *streamRelay) waitCredit() bool {
	var nextLog time.Time
	for {
		s.mu.Lock()
		ok := s.ring.End()-s.ack < outputWindowBytes
		s.mu.Unlock()
		if ok {
			return true
		}
		now := time.Now()
		if nextLog.IsZero() {
			nextLog = now.Add(s.stallAfter)
		}
		if !now.Before(nextLog) {
			s.mu.Lock()
			ack, end, detached := s.ack, s.ring.End(), s.detachedSince
			s.mu.Unlock()
			log.Printf("eterm daemon stream %d output stalled ack=%d ringEnd=%d detachedSince=%v", s.sidV.Load(), ack, end, detached)
			nextLog = now.Add(s.stallInterval)
		}
		timer := time.NewTimer(time.Until(nextLog))
		select {
		case <-s.wake:
			timer.Stop()
		case <-s.stop:
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (s *streamRelay) readPump(readDone chan<- error) {
	defer recoverLog(func() string { return fmt.Sprintf("stream %d read pump", s.sidV.Load()) })
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
	defer recoverLog(func() string { return fmt.Sprintf("stream %d pump", s.sidV.Load()) })
	s.sidV.Store(streamID)
	readDone := make(chan error, 1)
	go s.readPump(readDone)
	var endErr error
	ended := false
	for {
		sender := mgr.sender()
		s.mu.Lock()
		sent, end, inflight := s.sent, s.ring.End(), s.sent-s.ack
		if sender != nil && sent < end && inflight < outputWindowBytes {
			sid, gen := s.sidV.Load(), s.gen
			n := int(end - sent)
			if n > maxOutputFrameBytes {
				n = maxOutputFrameBytes
			}
			frame, data := relay.DataFrameBuf(sid, sent, n)
			s.ring.ReadInto(sent, data)
			s.sent += uint64(n)
			s.mu.Unlock()
			if err := sender.sendData(frame); err != nil {
				s.mu.Lock()
				if s.gen == gen {
					s.sent = sent
				}
				s.mu.Unlock()
				select {
				case <-time.After(sendRetryDelay):
				case <-s.stop:
					return
				case <-ctx.Done():
					return
				}
				continue
			}
			s.drainIfGenChanged(gen, sid, sender)
			continue
		}
		s.mu.Unlock()
		if ended && sender != nil && sent >= end {
			if closeID, ok := mgr.removeStream(s); ok {
				s.shutdown()
				_ = s.is.Close()
				_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: closeID, Payload: closePayload(endErr)})
				log.Printf("eterm daemon stream %d closed err=%v", closeID, endErr)
			}
			return
		}
		select {
		case err := <-readDone:
			endErr = sessionDoneErr(err, s.is.Done)
			ended = true
			log.Printf("eterm daemon stream %d read pump exited err=%v", s.sidV.Load(), err)
		case err := <-s.is.Done:
			endErr = err
			ended = true
			log.Printf("eterm daemon stream %d session exited err=%v", s.sidV.Load(), err)
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
	attachMu sync.Mutex
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
