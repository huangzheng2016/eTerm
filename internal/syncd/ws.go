package syncd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

// laneQueue is a per-direction send queue. Control frames (open/close/resize/
// ack/input) drain before bulk daemon output, and a full queue blocks the
// forwarding read loop, applying backpressure instead of killing the session.
// done is closed when the owning connection tears down, so forwarders blocked
// on a dead peer's full queue wake up instead of stalling their own read loop.
type laneQueue struct {
	ctrl chan relay.Frame
	bulk chan relay.Frame
	done chan struct{}
}

const relaySendQueueSize = 1024

func newLaneQueue() *laneQueue {
	return &laneQueue{
		ctrl: make(chan relay.Frame, relaySendQueueSize),
		bulk: make(chan relay.Frame, relaySendQueueSize),
		done: make(chan struct{}),
	}
}

// close wakes forwarders blocked in send; called once by the owning
// connection's teardown.
func (q *laneQueue) close() { close(q.done) }

// send blocks until the frame is queued, ctx is done, or the queue's owner
// connection closed; a full queue pauses the caller's read loop, which is the
// relay backpressure mechanism.
func (q *laneQueue) send(ctx context.Context, f relay.Frame, bulk bool) bool {
	ch := q.ctrl
	if bulk {
		ch = q.bulk
	}
	select {
	case ch <- f:
		return true
	case <-ctx.Done():
		return false
	case <-q.done:
		return false
	}
}

// sendCtl best-effort queues a control frame during teardown.
func (q *laneQueue) sendCtl(f relay.Frame) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case q.ctrl <- f:
	case <-q.done:
	case <-timer.C:
	}
}

type relaySession struct {
	client *laneQueue
	daemon *laneQueue
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

// closeClientSessions drops the client's stream mappings and tells the daemon
// side the client went away; the daemon keeps the PTY alive and buffers
// output so the client can resume the stream after reconnecting.
func (h *RelayHub) closeClientSessions(client *laneQueue) {
	h.mu.Lock()
	var daemons []*laneQueue
	var ids []uint32
	for id, s := range h.sessions {
		if s.client == client {
			daemons = append(daemons, s.daemon)
			ids = append(ids, id)
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for i, d := range daemons {
		d.sendCtl(relay.Frame{Type: relay.FrameClose, StreamID: ids[i], Payload: []byte(relay.CloseClientDisconnected)})
	}
}

func (h *RelayHub) closeDaemonSessions(daemon *laneQueue) {
	h.mu.Lock()
	var clients []*laneQueue
	var ids []uint32
	for id, s := range h.sessions {
		if s.daemon == daemon {
			clients = append(clients, s.client)
			ids = append(ids, id)
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for i, ch := range clients {
		ch.sendCtl(relay.Frame{Type: relay.FrameClose, StreamID: ids[i], Payload: []byte(relay.CloseDaemonDisconnected)})
	}
}

func (h *RelayHub) daemonWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	c.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer c.CloseNow()

	send := newLaneQueue()
	ctx := r.Context()
	done := make(chan struct{})
	stop := make(chan struct{})
	go writeWS(ctx, c, send, stop, done)

	var tenant, peerID string
	defer func() {
		if tenant != "" && peerID != "" {
			h.peers.UnregisterConn(tenant, peerID, send)
			log.Printf("syncd relay daemon unregistered tenant=%s peer=%s", shortID(tenant), peerID)
		}
		h.closeDaemonSessions(send)
		send.close()
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
			if hello.Version != relay.ProtocolVersion {
				send.sendCtl(relay.Frame{Type: relay.FrameHelloErr, Payload: []byte(fmt.Sprintf("unsupported protocol version %d, want %d", hello.Version, relay.ProtocolVersion))})
				return
			}
			tenant = hello.Tenant
			peerID = h.peers.Register(tenant, PeerInfo{ID: hello.PeerID, Name: hello.Name, LastSeen: time.Now()}, send)
			log.Printf("syncd relay daemon registered tenant=%s peer=%s name=%q", shortID(tenant), peerID, hello.Name)
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			// Daemon -> client: FrameData is bulk output, everything else is control.
			if !s.client.send(ctx, f, f.Type == relay.FrameData) {
				if ctx.Err() != nil {
					return
				}
				// Client connection is gone; drop the mapping and keep going.
				h.closeSession(f.StreamID)
				continue
			}
			if f.Type == relay.FrameClose || f.Type == relay.FrameOpenErr {
				h.closeSession(f.StreamID)
			}
		}
	}
}

func (h *RelayHub) clientWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	c.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer c.CloseNow()

	send := newLaneQueue()
	ctx := r.Context()
	done := make(chan struct{})
	stop := make(chan struct{})
	go writeWS(ctx, c, send, stop, done)
	defer func() {
		h.closeClientSessions(send)
		send.close()
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
			if json.Unmarshal(f.Payload, &hello) == nil && hello.Version != 0 && hello.Version != relay.ProtocolVersion {
				send.sendCtl(relay.Frame{Type: relay.FrameHelloErr, Payload: []byte(fmt.Sprintf("unsupported protocol version %d, want %d", hello.Version, relay.ProtocolVersion))})
				return
			}
			continue
		}
		if f.Type == relay.FrameOpen {
			var open relay.OpenRequest
			if json.Unmarshal(f.Payload, &open) != nil {
				send.sendCtl(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("bad open payload")})
				continue
			}
			tenant := r.Header.Get("X-ETerm-Tenant")
			peer, ok := h.peers.Get(tenant, open.PeerID)
			if !ok {
				log.Printf("syncd relay peer offline tenant=%s peer=%s available=%v", shortID(tenant), open.PeerID, h.peers.IDs(tenant))
				send.sendCtl(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("peer offline")})
				continue
			}
			if err := h.setSession(f.StreamID, relaySession{client: send, daemon: peer.Send}); err != nil {
				send.sendCtl(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
				continue
			}
			if !peer.Send.send(ctx, f, false) {
				if ctx.Err() != nil {
					return
				}
				h.closeSession(f.StreamID)
				continue
			}
			continue
		}
		if s, ok := h.session(f.StreamID); ok {
			// Client -> daemon: input and control frames are all interactive priority.
			if !s.daemon.send(ctx, f, false) {
				if ctx.Err() != nil {
					return
				}
				// Daemon connection is gone; drop the mapping and keep going.
				h.closeSession(f.StreamID)
				continue
			}
			if f.Type == relay.FrameClose {
				h.closeSession(f.StreamID)
			}
		}
	}
}

func shortID(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func writeWS(ctx context.Context, c *websocket.Conn, q *laneQueue, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	write := func(f relay.Frame) bool {
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := c.Write(wctx, websocket.MessageBinary, relay.Encode(f))
		cancel()
		return err == nil
	}
	for {
		var f relay.Frame
		select {
		case f = <-q.ctrl:
		default:
			select {
			case f = <-q.ctrl:
			case f = <-q.bulk:
			case <-stop:
				// Flush queued control frames (e.g. HelloErr, Close) before exit.
				for {
					select {
					case f := <-q.ctrl:
						if !write(f) {
							return
						}
					default:
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
		if !write(f) {
			return
		}
	}
}
