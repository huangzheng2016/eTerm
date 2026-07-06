package remote

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
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
}

const (
	wsKeepaliveInterval = 25 * time.Second
	wsKeepaliveTimeout  = 5 * time.Second
)

func Open(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, hostSyncID string, rows, cols int) (*internalssh.InteractiveSession, error) {
	return OpenWithProgress(ctx, serverURL, apiKey, tenant, insecureTLS, peerID, target, hostSyncID, rows, cols, nil)
}

func OpenWithProgress(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, hostSyncID string, rows, cols int, progress ProgressFunc) (*internalssh.InteractiveSession, error) {
	conn, streamID, _, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: target, HostSyncID: hostSyncID, Rows: rows, Cols: cols}, progress)
	if err != nil {
		return nil, err
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols), nil
}

func OpenTmuxSession(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int) (*internalssh.InteractiveSession, string, error) {
	return OpenTmuxSessionWithProgress(ctx, serverURL, apiKey, tenant, insecureTLS, peerID, target, sessionID, rows, cols, nil)
}

func OpenTmuxSessionWithProgress(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress ProgressFunc) (*internalssh.InteractiveSession, string, error) {
	conn, streamID, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, relay.OpenRequest{PeerID: peerID, Target: target, SessionID: sessionID, Rows: rows, Cols: cols}, progress)
	if err != nil {
		return nil, "", err
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols), string(okPayload), nil
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

func openControl(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest) ([]byte, error) {
	conn, _, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, op, nil)
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

func openStream(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op relay.OpenRequest, progress ProgressFunc) (*websocket.Conn, uint32, []byte, error) {
	ctx, cancel := openTimeoutContext(ctx)
	defer cancel()

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
		return nil, 0, nil, err
	}
	streamID := randomStreamID()
	payload, _ := json.Marshal(op)
	reportOpenProgress(progress, OpenStageRequest)
	if err := writeFrame(ctx, conn, relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload}); err != nil {
		conn.CloseNow()
		return nil, 0, nil, err
	}
	reportOpenProgress(progress, OpenStageReply)
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, 0, nil, err
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := relay.Decode(data)
		if err != nil || f.StreamID != streamID {
			continue
		}
		if f.Type == relay.FrameOpenErr {
			conn.CloseNow()
			return nil, 0, nil, errors.New(string(f.Payload))
		}
		if f.Type == relay.FrameOpenOK {
			return conn, streamID, f.Payload, nil
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

func sessionFromConn(ctx context.Context, conn *websocket.Conn, streamID uint32, rows, cols int) *internalssh.InteractiveSession {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	keepaliveCtx, stopKeepalive := context.WithCancel(ctx)
	wskeepalive.Start(keepaliveCtx, conn, wsKeepaliveInterval, wsKeepaliveTimeout)
	stdin := &wsStdin{ctx: ctx, conn: conn, streamID: streamID}
	is := &internalssh.InteractiveSession{
		Stdin:  stdin,
		Stdout: pr,
		Done:   done,
		Resize: func(rows, cols int) error {
			stdin.mu.Lock()
			defer stdin.mu.Unlock()
			return writeFrame(ctx, conn, relay.Frame{Type: relay.FrameResize, StreamID: streamID, Payload: relay.ResizePayload(rows, cols)})
		},
	}
	is.AddCloser(closerFunc(stopKeepalive))
	go func() {
		defer pw.Close()
		sawData := false
		for {
			typ, data, err := conn.Read(ctx)
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
				if len(f.Payload) > 0 {
					sawData = true
				}
				if _, err := pw.Write(f.Payload); err != nil {
					done <- err
					return
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
	err := writeFrame(w.ctx, w.conn, relay.Frame{Type: relay.FrameData, StreamID: w.streamID, Payload: p})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsStdin) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = writeFrame(w.ctx, w.conn, relay.Frame{Type: relay.FrameClose, StreamID: w.streamID})
	return w.conn.Close(websocket.StatusNormalClosure, "")
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
