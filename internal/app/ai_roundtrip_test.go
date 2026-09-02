package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ai"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
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
// the done handler like the aiToolSendKeysDoneMsg case does, following poll
// re-arms until the request is answered.
func serveAIToolRequests(a App, ch <-chan aiToolRequest) {
	for req := range ch {
		_, cmd := a.handleAIToolRequest(req)
		if cmd == nil {
			continue
		}
		go func(cmd tea.Cmd) {
			for cmd != nil {
				msg, ok := cmd().(aiToolSendKeysDoneMsg)
				if !ok {
					return
				}
				_, cmd = a.handleAIToolSendKeysDone(msg)
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

// sendKeysToPty runs one SendKeys call against a fake pty and returns the
// bytes written to its stdin.
func sendKeysToPty(t *testing.T, keys string) string {
	t.Helper()
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

	tabs, err := exec.ListTabs(context.Background())
	if err != nil || len(tabs) != 1 {
		t.Fatalf("ListTabs = %+v %v", tabs, err)
	}
	if _, err := exec.SendKeys(context.Background(), tabs[0].ID, keys, 5); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for sink.String() == "" {
		if time.Now().After(deadline) {
			t.Fatal("nothing written to pty stdin")
		}
		time.Sleep(time.Millisecond)
	}
	return sink.String()
}

func TestSendKeysEscapeDecoding(t *testing.T) {
	if got := sendKeysToPty(t, `\n`); got != "\n" {
		t.Fatalf(`backslash+n wrote %q, want one LF byte`, got)
	}
	if got := sendKeysToPty(t, `\\n`); got != `\n` {
		t.Fatalf(`backslash-backslash-n wrote %q, want literal backslash+n`, got)
	}
	if got := sendKeysToPty(t, "real\n"); got != "real\n" {
		t.Fatalf("raw newline wrote %q, want unchanged", got)
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

// cronFireAgent records Run inputs on top of fakeAgent.
type cronFireAgent struct {
	fakeAgent
	runs []string
}

func (a *cronFireAgent) Run(ctx context.Context, input string) <-chan ai.Event {
	a.runs = append(a.runs, input)
	return a.fakeAgent.Run(ctx, input)
}

// The fullscreen AI panel must sit at overlay origin (0,0) exactly: clicks
// outside overlayBounds dismiss the panel, so any gap makes edge clicks
// close it and shifts every drag selection.
func TestAIOverlayBoundsFillFrame(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"})
	if err := store.SetActive("p", "m"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store}
	av := aiview.New(bridge, bridge, bridge)
	av.SetSize(100, 32)
	a := App{aiView: av, width: 100, height: 32}
	ox, oy, ow, oh := a.overlayBounds(a.aiView.View().Content)
	if ox != 0 || oy != 0 || ow != 100 || oh != 32 {
		t.Fatalf("overlayBounds = %d,%d %dx%d, want 0,0 100x32", ox, oy, ow, oh)
	}
}

// The notify tool request is answered and emitted as an OSC 9 raw sequence.
func TestAINotifyEmitsOSC9(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"})
	if err := store.SetActive("p", "m"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store}
	a := App{aiView: aiview.New(bridge, bridge, bridge)}
	req := aiToolRequest{op: aiToolNotify, arg: "task done", resp: make(chan aiToolResult, 1)}
	_, cmd := a.handleAIToolRequest(req)
	if cmd == nil {
		t.Fatal("notify returned no cmd")
	}
	msg, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want tea.RawMsg", cmd())
	}
	if raw, _ := msg.Msg.(string); raw != "\x1b]9;task done\a" {
		t.Fatalf("raw = %q", raw)
	}
	select {
	case <-req.resp:
	default:
		t.Fatal("notify request not answered")
	}
}

// A cron fire routed through the panel's send path starts a new run when the
// panel is idle and queues onto the active run when one is in flight.
func TestAICronFireDelivery(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"})
	if err := store.SetActive("p", "m"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store}
	agent := &cronFireAgent{}
	bridge.agent = agent
	bridge.agentKey = "p\x00m\x00false"
	a := App{aiView: aiview.New(bridge, bridge, bridge)}
	defer bridge.CancelRun()

	_, cmd := a.handleAIToolRequest(aiToolRequest{op: aiToolCronFire, arg: "wake one"})
	if cmd == nil {
		t.Fatal("idle fire did not start a run")
	}
	if !bridge.running {
		t.Fatal("bridge not running after idle fire")
	}
	if len(agent.runs) != 1 || agent.runs[0] != "wake one" {
		t.Fatalf("runs: %v", agent.runs)
	}

	_, cmd = a.handleAIToolRequest(aiToolRequest{op: aiToolCronFire, arg: "wake two"})
	if cmd != nil {
		t.Fatal("queued fire returned a command")
	}
	if len(agent.runs) != 1 {
		t.Fatalf("queued fire started a second run: %v", agent.runs)
	}
	if len(agent.queued) != 1 || agent.queued[0] != "wake two" {
		t.Fatalf("queued: %v", agent.queued)
	}
}
