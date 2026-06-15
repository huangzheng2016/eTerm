package syncd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

type wsHello struct {
	Role    string `json:"role"`
	Tenant  string `json:"tenant"`
	PeerID  string `json:"peer_id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type wsOpen struct {
	PeerID     string `json:"peer_id"`
	Target     string `json:"target"`
	HostSyncID string `json:"host_sync_id,omitempty"`
}

type relaySession struct {
	client chan relay.Frame
	daemon chan relay.Frame
}

type RelayHub struct {
	peers    *PeerRegistry
	mu       sync.Mutex
	sessions map[uint32]relaySession
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
			frames = append(frames, closeFrame{ch: s.client, f: relay.Frame{Type: relay.FrameClose, StreamID: id}})
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for _, cf := range frames {
		trySend(cf.ch, cf.f)
	}
}

func trySend(ch chan relay.Frame, f relay.Frame) {
	select {
	case ch <- f:
	default:
	}
}

func (h *RelayHub) daemonWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()

	send := make(chan relay.Frame, 64)
	ctx := r.Context()
	done := make(chan struct{})
	go writeWS(ctx, c, send, done)

	var tenant, peerID string
	defer func() {
		if tenant != "" && peerID != "" {
			h.peers.Unregister(tenant, peerID)
		}
		h.closeDaemonSessions(send)
		close(send)
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
			var hello wsHello
			if json.Unmarshal(f.Payload, &hello) != nil || hello.PeerID == "" {
				continue
			}
			tenant, peerID = hello.Tenant, hello.PeerID
			h.peers.Register(tenant, PeerInfo{ID: hello.PeerID, Name: hello.Name, LastSeen: time.Now()}, send)
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			select {
			case s.client <- f:
			case <-ctx.Done():
				return
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

	send := make(chan relay.Frame, 64)
	ctx := r.Context()
	done := make(chan struct{})
	go writeWS(ctx, c, send, done)
	defer func() {
		h.closeClientSessions(send)
		close(send)
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
			var open wsOpen
			if json.Unmarshal(f.Payload, &open) != nil {
				send <- relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("bad open payload")}
				continue
			}
			peer, ok := h.peers.Get(r.Header.Get("X-ETerm-Tenant"), open.PeerID)
			if !ok {
				send <- relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("peer offline")}
				continue
			}
			if err := h.setSession(f.StreamID, relaySession{client: send, daemon: peer.Send}); err != nil {
				send <- relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())}
				continue
			}
			select {
			case peer.Send <- f:
			case <-ctx.Done():
				return
			}
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			select {
			case s.daemon <- f:
			case <-ctx.Done():
				return
			}
			if f.Type == relay.FrameClose {
				h.closeSession(f.StreamID)
			}
		}
	}
}

func writeWS(ctx context.Context, c *websocket.Conn, ch <-chan relay.Frame, done chan<- struct{}) {
	defer close(done)
	for f := range ch {
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := c.Write(wctx, websocket.MessageBinary, relay.Encode(f))
		cancel()
		if err != nil {
			return
		}
	}
}
