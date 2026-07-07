package syncd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

type relaySession struct {
	client chan relay.Frame
	daemon chan relay.Frame
}

type RelayHub struct {
	peers    *PeerRegistry
	mu       sync.Mutex
	sessions map[uint32]relaySession
}

const relaySendQueueSize = 1024

var relaySendTimeoutNanos atomic.Int64

func init() {
	relaySendTimeoutNanos.Store(int64(5 * time.Minute))
}

func NewRelayHub(peers *PeerRegistry) *RelayHub {
	return &RelayHub{
		peers:    peers,
		sessions: make(map[uint32]relaySession),
	}
}

func (h *RelayHub) setSession(id uint32, s relaySession) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessions[id]; ok {
		return fmt.Errorf("stream already exists")
	}
	h.sessions[id] = s
	return nil
}

func (h *RelayHub) session(id uint32) (relaySession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	return s, ok
}

func (h *RelayHub) closeSession(id uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

type closeFrame struct {
	ch chan relay.Frame
	f  relay.Frame
}

func (h *RelayHub) closeClientSessions(client chan relay.Frame) {
	h.mu.Lock()
	var frames []closeFrame
	for id, s := range h.sessions {
		if s.client == client {
			frames = append(frames, closeFrame{ch: s.daemon, f: relay.Frame{Type: relay.FrameClose, StreamID: id}})
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for _, cf := range frames {
		trySend(cf.ch, cf.f)
	}
}

func (h *RelayHub) closeDaemonSessions(daemon chan relay.Frame) {
	h.mu.Lock()
	var frames []closeFrame
	for id, s := range h.sessions {
		if s.daemon == daemon {
			frames = append(frames, closeFrame{ch: s.client, f: relay.Frame{Type: relay.FrameClose, StreamID: id, Payload: []byte(relay.CloseDaemonDisconnected)}})
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for _, cf := range frames {
		trySend(cf.ch, cf.f)
	}
}

func trySend(ch chan relay.Frame, f relay.Frame) bool {
	timer := time.NewTimer(time.Duration(relaySendTimeoutNanos.Load()))
	defer timer.Stop()
	select {
	case ch <- f:
		return true
	case <-timer.C:
		return false
	}
}

func (h *RelayHub) daemonWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()

	send := make(chan relay.Frame, relaySendQueueSize)
	ctx := r.Context()
	done := make(chan struct{})
	stop := make(chan struct{})
	go writeWS(ctx, c, send, stop, done)

	var tenant, peerID string
	defer func() {
		if tenant != "" && peerID != "" {
			h.peers.Unregister(tenant, peerID)
		}
		h.closeDaemonSessions(send)
		close(stop)
		<-done
	}()

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := relay.Decode(data)
		if err != nil {
			continue
		}
		if f.Type == relay.FrameHello {
			var hello relay.HelloPayload
			if json.Unmarshal(f.Payload, &hello) != nil || hello.PeerID == "" {
				continue
			}
			tenant = hello.Tenant
			peerID = h.peers.Register(tenant, PeerInfo{ID: hello.PeerID, Name: hello.Name, LastSeen: time.Now()}, send)
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			if !trySend(s.client, f) {
				h.closeSession(f.StreamID)
				_ = trySend(s.daemon, relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
				continue
			}
			if f.Type == relay.FrameClose || f.Type == relay.FrameOpenErr {
				h.closeSession(f.StreamID)
			}
		}
	}
}

func (h *RelayHub) clientWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()

	send := make(chan relay.Frame, relaySendQueueSize)
	ctx := r.Context()
	done := make(chan struct{})
	stop := make(chan struct{})
	go writeWS(ctx, c, send, stop, done)
	defer func() {
		h.closeClientSessions(send)
		close(stop)
		<-done
	}()

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := relay.Decode(data)
		if err != nil {
			continue
		}
		if f.Type == relay.FrameHello {
			continue
		}
		if f.Type == relay.FrameOpen {
			var open relay.OpenRequest
			if json.Unmarshal(f.Payload, &open) != nil {
				_ = trySend(send, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("bad open payload")})
				continue
			}
			peer, ok := h.peers.Get(r.Header.Get("X-ETerm-Tenant"), open.PeerID)
			if !ok {
				_ = trySend(send, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("peer offline")})
				continue
			}
			if err := h.setSession(f.StreamID, relaySession{client: send, daemon: peer.Send}); err != nil {
				_ = trySend(send, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
				continue
			}
			if !trySend(peer.Send, f) {
				h.closeSession(f.StreamID)
				_ = trySend(send, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("daemon queue full")})
			}
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			if !trySend(s.daemon, f) {
				h.closeSession(f.StreamID)
				_ = trySend(s.client, relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("daemon queue full")})
				continue
			}
			if f.Type == relay.FrameClose {
				h.closeSession(f.StreamID)
			}
		}
	}
}

func writeWS(ctx context.Context, c *websocket.Conn, ch <-chan relay.Frame, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case f := <-ch:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageBinary, relay.Encode(f))
			cancel()
			if err != nil {
				return
			}
		case <-stop:
			return
		case <-ctx.Done():
			return
		}
	}
}
