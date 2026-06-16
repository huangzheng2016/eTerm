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
)

type openPayload struct {
	PeerID     string `json:"peer_id"`
	Target     string `json:"target"`
	HostSyncID string `json:"host_sync_id,omitempty"`
}

type wsStdin struct {
	ctx      context.Context
	conn     *websocket.Conn
	streamID uint32
	mu       sync.Mutex
}

func Open(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, hostSyncID string, rows, cols int) (*internalssh.InteractiveSession, error) {
	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}
	if tenant != "" {
		header.Set("X-ETerm-Tenant", tenant)
	}
	conn, err := dial(ctx, esync.WSURLCandidates(serverURL, "/api/v1/ws/client"), header, insecureTLS)
	if err != nil {
		return nil, err
	}
	streamID := randomStreamID()
	payload, _ := json.Marshal(openPayload{PeerID: peerID, Target: target, HostSyncID: hostSyncID})
	if err := conn.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload})); err != nil {
		conn.CloseNow()
		return nil, err
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, err
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
			return nil, errors.New(string(f.Payload))
		}
		if f.Type == relay.FrameOpenOK {
			return sessionFromConn(ctx, conn, streamID, rows, cols), nil
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
