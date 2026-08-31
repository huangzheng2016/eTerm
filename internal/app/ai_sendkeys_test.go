package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

func sendKeysTestApp(t *testing.T) (App, *sshview.Model) {
	t.Helper()
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	t.Cleanup(func() { _ = sv.Close() })
	a := App{tabs: []Tab{{Type: SSHTab, Title: "prod", Model: sv}}, aiShared: &aiSharedState{}}
	return a, sv
}

func sendKeysRequest(sv *sshview.Model, ctx context.Context) aiToolRequest {
	return aiToolRequest{
		op:     aiToolSendKeys,
		ctx:    ctx,
		id:     strconv.FormatUint(sv.StreamID(), 10),
		arg:    "make\r",
		waitMs: 1,
		resp:   make(chan aiToolResult, 1),
	}
}

// feedSSHChunk pushes pty output through the emulator, firing OSC callbacks.
func feedSSHChunk(m *sshview.Model, data string) {
	m.Update(sshview.ChunkMsg{StreamID: m.StreamID(), Data: []byte(data)})
}

func TestSendKeysWaitsForCommandEnd(t *testing.T) {
	oldPoll := aiSendKeysPollInterval
	aiSendKeysPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { aiSendKeysPollInterval = oldPoll })

	a, sv := sendKeysTestApp(t)
	req := sendKeysRequest(sv, context.Background())
	_, cmd := a.handleAIToolRequest(req)
	if cmd == nil {
		t.Fatal("expected wait command")
	}

	// The shell starts the command after the send; the minimum wait alone
	// must not answer while it runs.
	feedSSHChunk(sv, "\x1b]133;C\a")
	msg, ok := cmd().(aiToolSendKeysDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	_, cmd = a.handleAIToolSendKeysDone(msg)
	if cmd == nil {
		t.Fatal("answered while the command was still running")
	}
	select {
	case r := <-req.resp:
		t.Fatalf("answered before 133;D: %+v", r)
	default:
	}

	// 133;D finishes the command; the next poll answers with its output.
	feedSSHChunk(sv, "build ok\r\n\x1b]133;D;0\a")
	msg, ok = cmd().(aiToolSendKeysDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	_, cmd = a.handleAIToolSendKeysDone(msg)
	if cmd != nil {
		t.Fatal("kept polling after command completion")
	}
	select {
	case r := <-req.resp:
		if r.err != nil {
			t.Fatalf("resp err = %v", r.err)
		}
		if !strings.Contains(r.text, "build ok") {
			t.Fatalf("tail = %q", r.text)
		}
	default:
		t.Fatal("no answer after 133;D")
	}
}

func TestSendKeysTimeoutFallback(t *testing.T) {
	oldMax, oldPoll := aiSendKeysMaxWait, aiSendKeysPollInterval
	aiSendKeysMaxWait, aiSendKeysPollInterval = 80*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { aiSendKeysMaxWait, aiSendKeysPollInterval = oldMax, oldPoll })

	a, sv := sendKeysTestApp(t)
	// The shell emitted OSC 133 before, so the wait tracks command
	// completion; this command never reports 133;D.
	feedSSHChunk(sv, "\x1b]133;C\a\x1b]133;D;0\a")

	req := sendKeysRequest(sv, context.Background())
	_, cmd := a.handleAIToolRequest(req)

	start := time.Now()
	var resp aiToolResult
	answered := false
	for !answered {
		if cmd == nil {
			select {
			case resp = <-req.resp:
				answered = true
			case <-time.After(5 * time.Second):
				t.Fatal("send_keys never answered")
			}
			continue
		}
		select {
		case resp = <-req.resp:
			answered = true
			continue
		default:
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("send_keys never answered")
		}
		msg, ok := cmd().(aiToolSendKeysDoneMsg)
		if !ok {
			t.Fatalf("unexpected msg %T", msg)
		}
		_, cmd = a.handleAIToolSendKeysDone(msg)
	}
	if resp.err != nil {
		t.Fatalf("resp err = %v", resp.err)
	}
	if elapsed := time.Since(start); elapsed < aiSendKeysMaxWait-30*time.Millisecond {
		t.Fatalf("answered after %v, before the max wait %v", elapsed, aiSendKeysMaxWait)
	}
}

func TestSendKeysCtxCancel(t *testing.T) {
	oldPoll := aiSendKeysPollInterval
	aiSendKeysPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { aiSendKeysPollInterval = oldPoll })

	a, sv := sendKeysTestApp(t)
	feedSSHChunk(sv, "\x1b]133;C\a\x1b]133;D;0\a")

	ctx, cancel := context.WithCancel(context.Background())
	req := sendKeysRequest(sv, ctx)
	_, cmd := a.handleAIToolRequest(req)

	msg, ok := cmd().(aiToolSendKeysDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	_, cmd = a.handleAIToolSendKeysDone(msg)
	if cmd == nil {
		t.Fatal("answered before cancel")
	}
	cancel()
	msg, ok = cmd().(aiToolSendKeysDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	_, _ = a.handleAIToolSendKeysDone(msg)
	select {
	case r := <-req.resp:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("resp err = %v", r.err)
		}
	default:
		t.Fatal("no answer after cancel")
	}
}

func TestSendKeysNoOSC133AnswersAfterMinWait(t *testing.T) {
	a, sv := sendKeysTestApp(t)
	req := sendKeysRequest(sv, context.Background())
	_, cmd := a.handleAIToolRequest(req)

	feedSSHChunk(sv, "dumb shell output\r\n")
	msg, ok := cmd().(aiToolSendKeysDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	_, cmd = a.handleAIToolSendKeysDone(msg)
	if cmd != nil {
		t.Fatal("kept polling a shell without OSC 133")
	}
	select {
	case r := <-req.resp:
		if r.err != nil || !strings.Contains(r.text, "dumb shell output") {
			t.Fatalf("resp = %+v", r)
		}
	default:
		t.Fatal("no answer for shell without OSC 133")
	}
}
