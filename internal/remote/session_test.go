package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

func TestOpenWritesDataFrames(t *testing.T) {
	got := make(chan relay.Frame, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("missing auth header")
		}
		if r.Header.Get("X-ETerm-Tenant") != "tenant-a" {
			t.Errorf("missing tenant header")
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		got <- f
		if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
			t.Error(err)
			return
		}
		for {
			_, data, err = c.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			f, err = relay.Decode(data)
			if err != nil {
				t.Error(err)
				return
			}
			if f.Type == relay.FrameData {
				got <- f
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "token", "tenant-a", false, "peer-a", "local", "", 33, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	if _, err := is.Stdin.Write([]byte("echo ok\n")); err != nil {
		t.Fatal(err)
	}

	open := <-got
	if open.Type != relay.FrameOpen {
		t.Fatalf("got %v want OPEN", open.Type)
	}
	var payload struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := json.Unmarshal(open.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Rows != 33 || payload.Cols != 120 {
		t.Fatalf("open payload pty = %dx%d, want 33x120", payload.Rows, payload.Cols)
	}
	data := <-got
	if data.Type != relay.FrameData || string(data.Payload) != "echo ok\n" {
		t.Fatalf("got %#v, want DATA echo ok", data)
	}
}

func TestOpenReadsDataFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: []byte("remote")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 6)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "remote" {
		t.Fatalf("got %q want remote", string(buf))
	}
}

func TestOpenWithProgressReportsStages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
	}))
	defer server.Close()

	var got []OpenStage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := OpenWithProgress(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80, func(stage OpenStage) {
		got = append(got, stage)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	want := []OpenStage{OpenStageConnect, OpenStageRequest, OpenStageReply}
	if len(got) != len(want) {
		t.Fatalf("stages = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stages = %+v, want %+v", got, want)
		}
	}
}

func TestFrameClosePayloadEndsSessionWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("daemon disconnected")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	select {
	case err := <-is.Done:
		if err == nil || err.Error() != "daemon disconnected" {
			t.Fatalf("done err = %v, want daemon disconnected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
	}
}

func TestOpenTimeoutContextAddsDeadline(t *testing.T) {
	ctx, cancel := openTimeoutContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline")
	}
}

func TestParseShellList(t *testing.T) {
	got, err := ParseShellList([]byte(`[{"id":"ab","shell":"zsh","name":"work","created_unix":5,"busy":true}]`))
	if err != nil || len(got) != 1 || got[0].ID != "ab" || got[0].Name != "work" || !got[0].Busy {
		t.Fatalf("got %+v err %v", got, err)
	}
	empty, err := ParseShellList(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty parse: %+v %v", empty, err)
	}
}

func TestRenameActiveShell(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		var op openPayload
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetActiveRename || op.ShellID != "x1" || op.Name != "work" {
			t.Errorf("bad rename request: %+v err=%v", op, err)
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RenameActiveShell(ctx, server.URL, "", "", false, "peer-a", "x1", "work"); err != nil {
		t.Fatal(err)
	}
}

func TestListActiveShells(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return
		}
		var op openPayload
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetActiveList {
			t.Errorf("bad list request: %v target=%s", err, op.Target)
		}
		list, _ := json.Marshal([]relay.ActiveShellInfo{{ID: "x1", Shell: "bash", CreatedUnix: 9, Busy: false}})
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: list}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shells, err := ListActiveShells(ctx, server.URL, "", "", false, "peer-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(shells) != 1 || shells[0].ID != "x1" || shells[0].Shell != "bash" {
		t.Fatalf("got %+v", shells)
	}
}
