package sync

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDialWebSocketRequestsCompression(t *testing.T) {
	extensions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extensions <- r.Header.Get("Sec-WebSocket-Extensions")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		typ, payload, err := conn.Read(r.Context())
		if err == nil {
			_ = conn.Write(r.Context(), typ, payload)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := DialWebSocket(ctx, []string{"ws" + strings.TrimPrefix(server.URL, "http")}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	payload := bytes.Repeat([]byte("\x1b[2J\x1b[Hterminal output\r\n"), 1024)
	if err := conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	_, got, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %d bytes, want %d", len(got), len(payload))
	}
	if got := <-extensions; !strings.Contains(got, "permessage-deflate") {
		t.Fatalf("Sec-WebSocket-Extensions = %q", got)
	}
}
