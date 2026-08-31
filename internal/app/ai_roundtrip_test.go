package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type syncWriteCloser struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *syncWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriteCloser) Close() error { return nil }

func (w *syncWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// serveAIToolRequests mimics the App.Update side of the tool bridge without a
// tea runtime: handle each request, and for send_keys run the wait tick and
// the done handler like the aiToolSendKeysDoneMsg case does.
func serveAIToolRequests(a App, ch <-chan aiToolRequest) {
	for req := range ch {
		_, cmd := a.handleAIToolRequest(req)
		if cmd == nil {
			continue
		}
		go func(cmd tea.Cmd) {
			if msg, ok := cmd().(aiToolSendKeysDoneMsg); ok {
				a.handleAIToolSendKeysDone(msg.req)
			}
		}(cmd)
	}
}

func TestAIExecutorRoundTrip(t *testing.T) {
	ch := make(chan aiToolRequest, 16)
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a := App{
		tabs:     []Tab{{Type: SSHTab, Title: "prod", Model: sv}},
		aiShared: &aiSharedState{},
	}
	exec := &aiExecutor{reqCh: ch, shared: a.aiShared}
	go serveAIToolRequests(a, ch)
	defer close(ch)

	ctx := context.Background()
	tabs, err := exec.ListTabs(ctx)
	if err != nil || len(tabs) != 1 || tabs[0].Title != "prod" || tabs[0].Type != string(SSHTab) {
		t.Fatalf("ListTabs = %+v %v", tabs, err)
	}

	// Feed output through the chunk path so the transcript has content.
	updated, _ := sv.Update(sshview.ChunkMsg{StreamID: sv.StreamID(), Data: []byte("hello ai\r\n")})
	sv = updated.(*sshview.Model)
	a.tabs[0].Model = sv

	text, total, err := exec.ReadTab(ctx, tabs[0].ID, 1024, 0)
	if err != nil || !strings.Contains(text, "hello ai") || total == 0 {
		t.Fatalf("ReadTab = %q/%d %v", text, total, err)
	}
	if _, _, err := exec.ReadTab(ctx, "tab-9", 1024, 0); err == nil {
		t.Fatal("ReadTab on unknown id did not fail")
	}

	screen, err := exec.SendKeys(ctx, tabs[0].ID, "ls -la\n", 5)
	if err != nil || !strings.Contains(screen, "hello ai") {
		t.Fatalf("SendKeys = %q %v", screen, err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(sink.String(), "ls -la\n") {
		if time.Now().After(deadline) {
			t.Fatalf("raw keys not written to pty stdin: %q", sink.String())
		}
		time.Sleep(time.Millisecond)
	}

	if err := exec.EnterDaemon(ctx, "no-such-daemon", ""); err == nil {
		t.Fatal("EnterDaemon on unknown daemon did not fail")
	}
}

func TestAIExecutorRoundTripCancel(t *testing.T) {
	exec := &aiExecutor{reqCh: make(chan aiToolRequest), shared: &aiSharedState{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := exec.ListTabs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
