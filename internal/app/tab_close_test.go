package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestCloseTabMessageKeepsListRoot(t *testing.T) {
	a := App{
		tabs: []Tab{
			{Type: SessionListTab, Title: "List"},
			{Type: SSHTab, Title: "ssh"},
		},
		activeTab: 0,
	}

	next, _ := a.Update(types.CloseTabMsg{Index: -1})
	got := next.(App)
	if len(got.tabs) != 2 || got.tabs[0].Type != SessionListTab {
		t.Fatalf("tabs=%+v", got.tabs)
	}
}

func TestCloseShortcutKeepsListRoot(t *testing.T) {
	a := App{
		tabs: []Tab{
			{Type: SSHTab, Title: "ssh"},
			{Type: ForwardTab, Title: "List"},
		},
		activeTab: 1,
	}

	got, cmd := a.closeCurrentTabIfAllowed()
	if len(got.tabs) != 2 || cmd != nil {
		t.Fatalf("tabs=%+v cmd=%v", got.tabs, cmd)
	}
}

func TestEditorCloseReturnsToItsList(t *testing.T) {
	tests := []struct {
		name   string
		editor TabType
		list   TabType
	}{
		{name: "host", editor: EditorTab, list: HomeTab},
		{name: "forward", editor: FwdEditorTab, list: ForwardTab},
		{name: "snippet", editor: SnippetEditorTab, list: SnippetTab},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := App{
				tabs: []Tab{
					{Type: tt.list, Title: "List"},
					{Type: SSHTab, Title: "shell"},
					{Type: tt.editor, Title: "Edit"},
				},
				activeTab: 2,
			}

			next, _ := a.Update(types.CloseTabMsg{Index: -1})
			got := next.(App)
			if len(got.tabs) != 2 || got.activeTab != 0 || got.tabs[0].Type != tt.list {
				t.Fatalf("active=%d tabs=%+v", got.activeTab, got.tabs)
			}
		})
	}
}

func TestEditorSaveReturnsToItsList(t *testing.T) {
	tests := []struct {
		name   string
		editor TabType
		list   TabType
		msg    tea.Msg
	}{
		{name: "host", editor: EditorTab, list: HomeTab, msg: types.HostSavedMsg{}},
		{name: "forward", editor: FwdEditorTab, list: ForwardTab, msg: types.ForwardRuleSavedMsg{}},
		{name: "snippet", editor: SnippetEditorTab, list: SnippetTab, msg: types.SnippetSavedMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := App{
				tabs: []Tab{
					{Type: tt.list, Title: "List"},
					{Type: SSHTab, Title: "shell"},
					{Type: tt.editor, Title: "Edit"},
				},
				activeTab: 2,
			}

			next, _ := a.Update(tt.msg)
			got := next.(App)
			if len(got.tabs) != 2 || got.activeTab != 0 || got.tabs[0].Type != tt.list {
				t.Fatalf("active=%d tabs=%+v", got.activeTab, got.tabs)
			}
		})
	}
}

func TestEditorSaveClosesEditorAfterTabSwitch(t *testing.T) {
	a := App{
		tabs: []Tab{
			{Type: ForwardTab, Title: "List"},
			{Type: FwdEditorTab, Title: "Edit"},
			{Type: SSHTab, Title: "shell"},
		},
		activeTab: 2,
	}

	next, _ := a.Update(types.ForwardRuleSavedMsg{})
	got := next.(App)
	if len(got.tabs) != 2 || got.activeTab != 0 || got.tabs[0].Type != ForwardTab || got.tabs[1].Type != SSHTab {
		t.Fatalf("active=%d tabs=%+v", got.activeTab, got.tabs)
	}
}

type blockingCloser struct {
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func newBlockingCloser() *blockingCloser {
	return &blockingCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *blockingCloser) Close() error {
	c.startOnce.Do(func() { close(c.started) })
	<-c.release
	return nil
}

func (c *blockingCloser) Release() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func TestCloseCurrentTabRemovesTabBeforeSessionCleanup(t *testing.T) {
	closer := newBlockingCloser()
	t.Cleanup(closer.Release)

	is := &internalssh.InteractiveSession{}
	is.AddCloser(closer)
	tab := sshview.New(is, "ssh", 0, viewkeys.SSHKeys{})
	a := App{
		tabs: []Tab{
			{Type: HomeTab, Title: "home"},
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 1,
	}

	next, cmd := a.closeCurrentTabIfAllowed()
	if cmd == nil {
		t.Fatal("expected cleanup command")
	}
	if len(next.tabs) != 1 || next.tabs[0].Type != HomeTab {
		t.Fatalf("tabs = %#v, want only home tab", next.tabs)
	}
	if next.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0", next.activeTab)
	}
	select {
	case <-closer.started:
		t.Fatal("session cleanup started before command ran")
	default:
	}

	done := make(chan struct{})
	go func() {
		cmd()
		close(done)
	}()

	select {
	case <-closer.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	select {
	case <-done:
		t.Fatal("cleanup finished before release")
	default:
	}

	closer.Release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
}
