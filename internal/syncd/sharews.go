package syncd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

// shareStateIdleTTL exceeds the daemon's 10 minute detached-stream TTL, so a
// pruned state would fail resume on the daemon anyway.
const shareStateIdleTTL = 11 * time.Minute

// shareStreamState carries a guest stream across connections of the same
// share token: the relay stream ID and the cumulative acked offset the guest
// has displayed, so a reconnecting guest resumes where it left off.
type shareStreamState struct {
	streamID  uint32
	acked     atomic.Uint64
	turn      chan struct{} // capacity 1; holds a value while no bridge owns the stream
	idleSince time.Time     // last bridge teardown, for pruning
}

// shareState returns the stream state for token, creating it (and pruning
// long-idle states) on first use. created reports whether it is fresh.
func (h *RelayHub) shareState(token string) (st *shareStreamState, created bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for k, s := range h.shareStates {
		if !s.idleSince.IsZero() && now.Sub(s.idleSince) > shareStateIdleTTL {
			delete(h.shareStates, k)
		}
	}
	if st = h.shareStates[token]; st != nil {
		st.idleSince = time.Time{}
		return st, false
	}
	id, err := randomStreamID()
	if err != nil {
		return nil, false
	}
	st = &shareStreamState{streamID: id, turn: make(chan struct{}, 1)}
	st.turn <- struct{}{}
	h.shareStates[token] = st
	return st, true
}

// dropShareState removes the state only if st is still the current state for
// token, so a stale owner cannot delete a newer guest's state.
func (h *RelayHub) dropShareState(token string, st *shareStreamState) {
	h.mu.Lock()
	if h.shareStates[token] == st {
		delete(h.shareStates, token)
	}
	h.mu.Unlock()
}

// registerShareConn claims the single active guest connection for token; a
// previous connection's channel is closed so it exits with reason "replaced".
// The returned release func drops the claim if it is still current.
func (h *RelayHub) registerShareConn(token string) (<-chan struct{}, func()) {
	h.mu.Lock()
	if old, ok := h.shareConns[token]; ok {
		close(old)
	}
	ch := make(chan struct{})
	h.shareConns[token] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if h.shareConns[token] == ch {
			delete(h.shareConns, token)
		}
		h.mu.Unlock()
	}
}

func randomStreamID() (uint32, error) {
	var sid [4]byte
	for {
		if _, err := rand.Read(sid[:]); err != nil {
			return 0, err
		}
		if id := binary.BigEndian.Uint32(sid[:]); id != 0 {
			return id, nil
		}
	}
}

type shareGuestMsg struct {
	T    string `json:"t"`
	D    string `json:"d"`
	Rows int    `json:"rows"`
	Cols int    `json:"cols"`
}

type shareHostMsg struct {
	T      string `json:"t"`
	D      string `json:"d,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Teardown causes decide what happens to the remote shell when a guest
// connection ends.
const (
	shareExitGuest    = iota // browser went away: detach, keep the PTY for resume
	shareExitReplaced        // a newer connection owns the stream now
	shareExitFatal           // share expired / session over: kill the PTY
)

// shareWS bridges a browser guest (JSON text frames) to a daemon peer as a
// relay client. The guest token is the only credential. Guest disconnects
// detach the remote shell (daemon keeps the PTY and buffers output); a
// reconnect for the same token resumes the same stream from the last acked
// offset. A second concurrent connection replaces the first and takes over
// the stream.
func (h *RelayHub) shareWS(engine *Engine, w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, err := engine.GetShareByToken(token)
	if err == ErrShareNotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer c.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	replaced, release := h.registerShareConn(token)
	defer release()

	dead := make(chan struct{})
	var deadOnce sync.Once
	cause := shareExitGuest
	dieWith := func(why int) {
		deadOnce.Do(func() {
			cause = why
			close(dead)
			c.CloseNow()
		})
	}
	defer dieWith(shareExitGuest)

	peer, ok := h.peers.Get(share.Tenant, share.PeerID)
	if !ok {
		writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "peer offline"})
		return
	}

	st, fresh := h.shareState(token)
	if st == nil {
		return
	}
	select {
	case <-st.turn:
	case <-replaced:
		writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "replaced"})
		dieWith(shareExitReplaced)
		return
	case <-ctx.Done():
		return
	}

	q := newLaneQueue()
	streamID := st.streamID
	resumeFrom := st.acked.Load()
	defer func() {
		// Rejoin dieWith so a cause set by the forwarder goroutine is
		// visible here through the Once.
		dieWith(shareExitGuest)
		h.closeSession(streamID)
		q.close()
		switch cause {
		case shareExitReplaced:
			// The replacing connection resumes the stream; leave the
			// daemon side untouched.
		case shareExitGuest:
			peer.Send.sendCtl(relay.Frame{Type: relay.FrameClose, StreamID: streamID, Payload: []byte(relay.CloseClientDisconnected)})
		default:
			peer.Send.sendCtl(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
			h.dropShareState(token, st)
		}
		h.mu.Lock()
		st.idleSince = time.Now()
		h.mu.Unlock()
		st.turn <- struct{}{}
	}()

	// openAndWait sends FrameOpen and waits for the daemon's OpenOK/OpenErr.
	openAndWait := func() (relay.Frame, bool) {
		open, _ := json.Marshal(relay.OpenRequest{
			PeerID:        share.PeerID,
			Target:        share.Target,
			SessionID:     share.SessionID,
			Name:          share.Name,
			Rows:          24,
			Cols:          80,
			ResumeFromSeq: resumeFrom,
		})
		if err := h.setSession(streamID, relaySession{client: q, daemon: peer.Send}); err != nil {
			return relay.Frame{}, false
		}
		if !peer.Send.send(ctx, relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: open}, false) {
			h.closeSession(streamID)
			return relay.Frame{}, false
		}
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case f := <-q.ctrl:
			return f, true
		case <-replaced:
			writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "replaced"})
			dieWith(shareExitReplaced)
			return relay.Frame{}, false
		case <-dead:
			return relay.Frame{}, false
		case <-ctx.Done():
			return relay.Frame{}, false
		case <-timer.C:
			return relay.Frame{}, false
		}
	}

	f, opened := openAndWait()
	if opened && f.Type == relay.FrameOpenErr && !fresh {
		// Resume refused (unknown stream or offset outside the retained
		// ring): fall back to a brand new session on a new stream ID.
		h.closeSession(streamID)
		id, err := randomStreamID()
		if err != nil {
			dieWith(shareExitFatal)
			return
		}
		streamID = id
		st.streamID = id
		st.acked.Store(0)
		resumeFrom = 0
		fresh = true
		f, opened = openAndWait()
	}
	if !opened {
		select {
		case <-dead:
			// Replaced or shutting down; cause already set.
		default:
			writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "peer offline"})
			dieWith(shareExitFatal)
		}
		return
	}
	if f.Type != relay.FrameOpenOK {
		reason := string(f.Payload)
		if reason == "" {
			reason = "open failed"
		}
		writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: reason})
		dieWith(shareExitFatal)
		return
	}

	go h.shareForward(ctx, c, q, peer.Send, st, streamID, share.ExpiresAt, replaced, dead, dieWith)

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg shareGuestMsg
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.T {
		case "in":
			raw, err := base64.StdEncoding.DecodeString(msg.D)
			if err != nil {
				continue
			}
			if !peer.Send.send(ctx, relay.Frame{Type: relay.FrameData, StreamID: streamID, Payload: raw}, false) {
				return
			}
		case "resize":
			rows := min(max(msg.Rows, 1), 65535)
			cols := min(max(msg.Cols, 1), 65535)
			peer.Send.send(ctx, relay.Frame{Type: relay.FrameResize, StreamID: streamID, Payload: relay.ResizePayload(rows, cols)}, false)
		}
	}
}

// shareForward drains relay frames from the daemon, translates them to guest
// JSON messages, acks consumed output, and ends the session at the share's
// fixed expiry. It never blocks on a stuck browser: writes carry a timeout
// and failure tears the connection down.
func (h *RelayHub) shareForward(ctx context.Context, c *websocket.Conn, q *laneQueue, daemon *laneQueue, st *shareStreamState, streamID uint32, expiresAt time.Time, replaced <-chan struct{}, dead <-chan struct{}, dieWith func(int)) {
	expiry := time.NewTimer(time.Until(expiresAt))
	defer expiry.Stop()
	for {
		var f relay.Frame
		select {
		case f = <-q.ctrl:
		default:
			select {
			case f = <-q.ctrl:
			case f = <-q.bulk:
			case <-expiry.C:
				writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "share expired"})
				dieWith(shareExitFatal)
				return
			case <-replaced:
				writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: "replaced"})
				dieWith(shareExitReplaced)
				return
			case <-dead:
				return
			case <-ctx.Done():
				return
			}
		}
		switch f.Type {
		case relay.FrameOpenOK:
		case relay.FrameData:
			seq, payload, err := relay.ParseData(f.Payload)
			if err != nil {
				continue
			}
			if !writeShareMsg(ctx, c, shareHostMsg{T: "out", D: base64.StdEncoding.EncodeToString(payload)}) {
				dieWith(shareExitGuest)
				return
			}
			if acked := seq + uint64(len(payload)); acked > st.acked.Load() {
				st.acked.Store(acked)
			}
			daemon.send(ctx, relay.Frame{Type: relay.FrameAck, StreamID: streamID, Payload: relay.AckPayload(seq + uint64(len(payload)))}, false)
		case relay.FrameOpenErr, relay.FrameHelloErr, relay.FrameClose:
			reason := string(f.Payload)
			if reason == "" {
				reason = "session closed"
			}
			writeShareMsg(ctx, c, shareHostMsg{T: "exit", Reason: reason})
			dieWith(shareExitFatal)
			return
		}
	}
}

func writeShareMsg(ctx context.Context, c *websocket.Conn, msg shareHostMsg) bool {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	return c.Write(wctx, websocket.MessageText, data) == nil
}
