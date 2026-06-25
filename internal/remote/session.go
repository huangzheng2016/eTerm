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

type openPayload struct {
	PeerID     string `json:"peer_id"`
	Target     string `json:"target"`
	HostSyncID string `json:"host_sync_id,omitempty"`
	ShellID    string `json:"shell_id,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Cols       int    `json:"cols,omitempty"`
}

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
	conn, streamID, _, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, openPayload{PeerID: peerID, Target: target, HostSyncID: hostSyncID, Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols), nil
}

// OpenActiveShell attaches to an existing daemon shell (target active-attach) or
// creates one (target active-new). For active-new the assigned shell id is
// returned in the second value.
func OpenActiveShell(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, shellID string, rows, cols int) (*internalssh.InteractiveSession, string, error) {
	conn, streamID, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, openPayload{PeerID: peerID, Target: target, ShellID: shellID, Rows: rows, Cols: cols})
	if err != nil {
		return nil, "", err
	}
	return sessionFromConn(ctx, conn, streamID, rows, cols), string(okPayload), nil
}

// ListActiveShells issues a one-shot active-list request.
func ListActiveShells(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID string) ([]relay.ActiveShellInfo, error) {
	conn, _, okPayload, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, openPayload{PeerID: peerID, Target: relay.TargetActiveList})
	if err != nil {
		return nil, err
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return ParseShellList(okPayload)
}

// KillActiveShell issues a one-shot active-kill request.
func KillActiveShell(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, shellID string) error {
	conn, _, _, err := openStream(ctx, serverURL, apiKey, tenant, insecureTLS, openPayload{PeerID: peerID, Target: relay.TargetActiveKill, ShellID: shellID})
	if err != nil {
		return err
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return nil
}

// ParseShellList decodes an active-list OpenOK payload.
func ParseShellList(payload []byte) ([]relay.ActiveShellInfo, error) {
	var out []relay.ActiveShellInfo
	if len(payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func openStream(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, op openPayload) (*websocket.Conn, uint32, []byte, error) {
	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}
	if tenant != "" {
		header.Set("X-ETerm-Tenant", tenant)
	}
	conn, err := dial(ctx, esync.WSURLCandidates(serverURL, "/api/v1/ws/client"), header, insecureTLS)
	if err != nil {
		return nil, 0, nil, err
	}
	streamID := randomStreamID()
	payload, _ := json.Marshal(op)
	if err := conn.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload})); err != nil {
		conn.CloseNow()
		return nil, 0, nil, err
	}
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

func dial(ctx context.Context, urls []string, header http.Header, insecureTLS bool) (*websocket.Conn, error) {
	var lastErr error
	client := esync.HTTPClient(30*time.Second, insecureTLS)
	for _, u := range urls {
		conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: header, HTTPClient: client})
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("server URL is required")
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
			return conn.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameResize, StreamID: streamID, Payload: relay.ResizePayload(rows, cols)}))
		},
	}
	is.AddCloser(closerFunc(stopKeepalive))
	go func() {
		defer pw.Close()
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
				if _, err := pw.Write(f.Payload); err != nil {
					done <- err
					return
				}
			case relay.FrameClose:
				done <- nil
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
	err := w.conn.Write(w.ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: w.streamID, Payload: p}))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsStdin) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.Write(w.ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: w.streamID}))
	return w.conn.Close(websocket.StatusNormalClosure, "")
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
