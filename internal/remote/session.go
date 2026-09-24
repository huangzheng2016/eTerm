package remote

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/wskeepalive"
)

type OpenStage string

const (
	OpenStageConnect OpenStage = "connect"
	OpenStageRequest OpenStage = "request"
	OpenStageReply   OpenStage = "reply"
)

type ProgressFunc func(OpenStage)

type wsStdin struct {
	ctx      context.Context
	conn     *websocket.Conn
	streamID uint32
	mu       sync.Mutex
	nextSeq  atomic.Uint64
	lastAck  atomic.Uint64
}

const (
	wsKeepaliveInterval = 25 * time.Second
	wsKeepaliveTimeout  = 5 * time.Second
	ackThresholdBytes   = 256 * 1024
	maxInputChunkBytes  = relay.MaxWebSocketMessageBytes - 1024
)

func Open(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, hostSyncID string, rows, cols int) (*internalssh.InteractiveSession, error) {
	return OpenWithProgress(ctx, serverURL, apiKey, tenant, insecureTLS, peerID, target, hostSyncID, rows, cols, nil)
}

func OpenWithProgress(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, hostSyncID string, rows, cols int, progress ProgressFunc) (*internalssh.InteractiveSession, error) {
	conn, streamID, _, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: target, HostSyncID: hostSyncID, Rows: rows, Cols: cols}, randomStreamID(), progress)
	if err != nil {
		return nil, err
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols, 0), nil
}

func ResumeOpenWithProgress(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest, streamID uint32, resumeFromSeq uint64, progress ProgressFunc) (*internalssh.InteractiveSession, error) {
	op.ResumeFromSeq = resumeFromSeq
	conn, _, _, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, op, streamID, progress)
	if err != nil {
		return nil, err
	}
	return sessionFromConn(ctx, conn, streamID, op.Rows, op.Cols, resumeFromSeq), nil
}

func ResumeInfo(is *internalssh.InteractiveSession) (streamID uint32, nextSeq uint64, ok bool) {
	if is == nil {
		return 0, 0, false
	}
	w, isWS := is.Stdin.(*wsStdin)
	if !isWS {
		return 0, 0, false
	}
	return w.streamID, w.nextSeq.Load(), true
}

func OpenTmuxSession(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int) (*internalssh.InteractiveSession, relay.TmuxSessionInfo, error) {
	return OpenTmuxSessionWithProgress(ctx, serverURL, apiKey, tenant, insecureTLS, peerID, target, sessionID, rows, cols, nil)
}

func OpenTmuxSessionWithProgress(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress ProgressFunc) (*internalssh.InteractiveSession, relay.TmuxSessionInfo, error) {
	conn, streamID, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: target, SessionID: sessionID, Rows: rows, Cols: cols}, randomStreamID(), progress)
	if err != nil {
		return nil, relay.TmuxSessionInfo{}, err
	}
	var info relay.TmuxSessionInfo
	if len(okPayload) != 0 {
		if err := json.Unmarshal(okPayload, &info); err != nil {
			_ = conn.Close(websocket.StatusProtocolError, "invalid tmux session")
			return nil, relay.TmuxSessionInfo{}, fmt.Errorf("decode tmux session: %w", err)
		}
	}
	if target == relay.TargetTmuxNew && (info.Name == "" || info.SessionID == "") {
		_ = conn.Close(websocket.StatusProtocolError, "missing tmux session identity")
		return nil, relay.TmuxSessionInfo{}, errors.New("tmux-new response missing session identity")
	}
	if info.SessionID == "" {
		info.SessionID = sessionID
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols, 0), info, nil
}

func ListTmuxSessions(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID string) ([]relay.TmuxSessionInfo, error) {
	okPayload, err := openControl(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: relay.TargetTmuxList})
	if err != nil {
		return nil, err
	}
	return ParseTmuxSessionList(okPayload)
}

func KillTmuxSession(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, sessionID string) error {
	_, err := openControl(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: relay.TargetTmuxKill, SessionID: sessionID})
	return err
}

func RenameTmuxSession(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, sessionID, name string) error {
	_, err := openControl(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: relay.TargetTmuxRename, SessionID: sessionID, Name: name})
	return err
}

func RenamePeer(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, name string) error {
	_, err := openControl(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: relay.TargetPeerRename, Name: name})
	return err
}

func openControl(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest) ([]byte, error) {
	conn, _, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, op, randomStreamID(), nil)
	if err != nil {
		return nil, err
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return okPayload, nil
}

func ParseTmuxSessionList(payload []byte) ([]relay.TmuxSessionInfo, error) {
	var out []relay.TmuxSessionInfo
	if len(payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func openStream(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest, streamID uint32, progress ProgressFunc) (*websocket.Conn, uint32, []byte, error) {
	ctx, cancel := openTimeoutContext(ctx)
	defer cancel()

	for {
		conn, payload, err := openStreamOnce(ctx, serverURL, apiKey, tenant, insecureTLS, op, streamID, progress)
		if !isPeerOffline(err) {
			return conn, streamID, payload, err
		}
		select {
		case <-time.After(peerOfflineRetryDelay):
		case <-ctx.Done():
			return nil, 0, nil, err
		}
	}
}

func openStreamOnce(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest, streamID uint32, progress ProgressFunc) (*websocket.Conn, []byte, error) {
	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}
	if tenant != "" {
		header.Set("X-ETerm-Tenant", tenant)
	}
	reportOpenProgress(progress, OpenStageConnect)
	conn, err := esync.DialWebSocket(ctx, esync.WSURLCandidates(serverURL, "/api/v1/ws/client"), header, insecureTLS)
	if err != nil {
		return nil, nil, err
	}
	hello, _ := json.Marshal(relay.HelloPayload{Role: "client", Version: relay.ProtocolVersion})
	if err := writeFrame(ctx, conn, relay.Frame{Type: relay.FrameHello, Payload: hello}); err != nil {
		conn.CloseNow()
		return nil, nil, err
	}
	payload, _ := json.Marshal(op)
	reportOpenProgress(progress, OpenStageRequest)
	if err := writeFrame(ctx, conn, relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload}); err != nil {
		conn.CloseNow()
		return nil, nil, err
	}
	reportOpenProgress(progress, OpenStageReply)
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, nil, err
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := relay.Decode(data)
		if err != nil {
			continue
		}
		if f.Type == relay.FrameHelloErr {
			conn.CloseNow()
			return nil, nil, fmt.Errorf("relay protocol rejected: %s", string(f.Payload))
		}
		if f.StreamID != streamID {
			continue
		}
		if f.Type == relay.FrameOpenErr {
			conn.CloseNow()
			return nil, nil, errors.New(string(f.Payload))
		}
		if f.Type == relay.FrameOpenOK {
			return conn, f.Payload, nil
		}
	}
}

func reportOpenProgress(progress ProgressFunc, stage OpenStage) {
	if progress != nil {
		progress(stage)
	}
}

var defaultOpenTimeout = 30 * time.Second
var defaultWriteTimeout = 10 * time.Second
var peerOfflineRetryDelay = time.Second

func isPeerOffline(err error) bool {
	return err != nil && err.Error() == "peer offline"
}

func openTimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultOpenTimeout)
}

func writeTimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultWriteTimeout)
}

func sessionFromConn(ctx context.Context, conn *websocket.Conn, streamID uint32, rows, cols int, resumeFromSeq uint64) *internalssh.InteractiveSession {
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	keepaliveCtx, stopKeepalive := context.WithCancel(sessionCtx)
	wskeepalive.Start(keepaliveCtx, conn, wsKeepaliveInterval, wsKeepaliveTimeout)
	stdin := &wsStdin{ctx: sessionCtx, conn: conn, streamID: streamID}
	stdin.nextSeq.Store(resumeFromSeq)
	stdin.lastAck.Store(resumeFromSeq)
	is := &internalssh.InteractiveSession{
		Stdin:  stdin,
		Stdout: pr,
		Done:   done,
		Resize: func(rows, cols int) error {
			stdin.mu.Lock()
			defer stdin.mu.Unlock()
			return writeFrame(sessionCtx, conn, relay.Frame{Type: relay.FrameResize, StreamID: streamID, Payload: relay.ResizePayload(rows, cols)})
		},
	}
	is.AddCloser(closerFunc(cancelSession))
	is.AddCloser(closerFunc(stopKeepalive))
	go func() {
		defer pw.Close()
		defer conn.CloseNow()
		sawData := false
		accepted := false
		for {
			typ, data, err := conn.Read(sessionCtx)
			if err != nil {
				done <- err
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			f, err := relay.Decode(data)
			if err != nil || f.StreamID != streamID {
				continue
			}
			switch f.Type {
			case relay.FrameData:
				seq, payload, err := relay.ParseData(f.Payload)
				if err != nil {
					continue
				}
				next := stdin.nextSeq.Load()
				if seq < next {
					log.Printf("eterm remote: dropping duplicate frame stream=%d seq=%d next=%d", streamID, seq, next)
					continue
				}
				if seq > next {
					if accepted {
						log.Printf("eterm remote: output gap stream=%d seq=%d want=%d", streamID, seq, next)
						done <- fmt.Errorf("relay output gap: got seq %d, want %d", seq, next)
						return
					}
					log.Printf("eterm remote: rebase stream=%d next=%d to first frame seq=%d", streamID, next, seq)
					stdin.nextSeq.Store(seq)
					stdin.lastAck.Store(seq)
					next = seq
				}
				accepted = true
				if len(payload) > 0 {
					sawData = true
				}
				if _, err := pw.Write(payload); err != nil {
					done <- err
					return
				}
				next = seq + uint64(len(payload))
				stdin.nextSeq.Store(next)
				if next-stdin.lastAck.Load() >= ackThresholdBytes {
					stdin.lastAck.Store(next)
					stdin.mu.Lock()
					_ = writeFrame(sessionCtx, conn, relay.Frame{Type: relay.FrameAck, StreamID: streamID, Payload: relay.AckPayload(next)})
					stdin.mu.Unlock()
				}
			case relay.FrameClose:
				if len(f.Payload) > 0 {
					done <- errors.New(string(f.Payload))
				} else if !sawData {
					done <- errors.New("remote terminal exited before output")
				} else {
					done <- nil
				}
				return
			}
		}
	}()
	_ = is.Resize(rows, cols)
	return is
}

type closerFunc func()

func (f closerFunc) Close() error {
	f()
	return nil
}

func (w *wsStdin) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sent := 0
	for {
		n := min(len(p), maxInputChunkBytes)
		if err := writeFrame(w.ctx, w.conn, relay.Frame{Type: relay.FrameData, StreamID: w.streamID, Payload: p[:n]}); err != nil {
			return sent, err
		}
		sent += n
		p = p[n:]
		if len(p) == 0 {
			return sent, nil
		}
	}
}

func (w *wsStdin) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = writeFrame(w.ctx, w.conn, relay.Frame{Type: relay.FrameClose, StreamID: w.streamID})
	return w.conn.Close(websocket.StatusNormalClosure, "")
}

func CloseSessionNow(is *internalssh.InteractiveSession) {
	if is == nil {
		return
	}
	if w, ok := is.Stdin.(*wsStdin); ok {
		w.conn.CloseNow()
	}
	_ = is.Close()
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f relay.Frame) error {
	wctx, cancel := writeTimeoutContext(ctx)
	defer cancel()
	return conn.Write(wctx, websocket.MessageBinary, relay.Encode(f))
}

func randomStreamID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 1
	}
	n := binary.BigEndian.Uint32(b[:])
	if n == 0 {
		return 1
	}
	return n
}
