package sync

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

func DialWebSocket(ctx context.Context, urls []string, header http.Header, insecureTLS bool) (*websocket.Conn, error) {
	var lastErr error
	client := HTTPClient(30*time.Second, insecureTLS)
	for _, u := range urls {
		conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
			HTTPHeader:      header,
			HTTPClient:      client,
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			lastErr = err
			continue
		}
		conn.SetReadLimit(relay.MaxWebSocketMessageBytes)
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("server URL is required")
}
