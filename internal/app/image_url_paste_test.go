package app

import (
	"bytes"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type testWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *testWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *testWriteCloser) Close() error { return nil }

func (w *testWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestImageUploadDonePastesIntoOriginalTab(t *testing.T) {
	firstStdin := &testWriteCloser{}
	secondStdin := &testWriteCloser{}
	first := sshview.New(&internalssh.InteractiveSession{Stdin: firstStdin}, "first", 0, viewkeys.SSHKeys{})
	second := sshview.New(&internalssh.InteractiveSession{Stdin: secondStdin}, "second", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = first.Close() })
	t.Cleanup(func() { _ = second.Close() })

	a := App{
		viewState: MainView,
		activeTab: 1,
		tabs: []Tab{
			{Type: SSHTab, Title: "first", Model: first},
			{Type: SSHTab, Title: "second", Model: second},
		},
	}

	updated, _ := a.Update(types.ImageUploadDoneMsg{StreamID: first.StreamID(), URL: "https://example.test/i.png", Filename: "i.png"})
	a = updated.(App)

	if got := firstStdin.String(); got != "[i.png](https://example.test/i.png) " {
		t.Fatalf("first stdin = %q", got)
	}
	if got := secondStdin.String(); got != "" {
		t.Fatalf("second stdin = %q", got)
	}
}

func TestImageUploadDoneCachesURL(t *testing.T) {
	stdin := &testWriteCloser{}
	tab := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "first", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	expiresAt := time.Now().Add(time.Minute)

	a := App{
		viewState: MainView,
		tabs: []Tab{
			{Type: SSHTab, Title: "first", Model: tab},
		},
	}

	updated, _ := a.Update(types.ImageUploadDoneMsg{
		StreamID:  tab.StreamID(),
		URL:       "https://example.test/b/abc",
		Filename:  "archive.tar.gz",
		CacheKey:  "image-key",
		ExpiresAt: expiresAt,
	})
	a = updated.(App)

	if got := a.imageURLCache["image-key"].URL; got != "https://example.test/b/abc" {
		t.Fatalf("cached url = %q", got)
	}
	if got := a.imageURLCache["image-key"].Filename; got != "archive.tar.gz" {
		t.Fatalf("cached filename = %q", got)
	}
}

func TestImagePasteFallbackForwardsOriginalPasteMsg(t *testing.T) {
	stdin := &testWriteCloser{}
	tab := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "first", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })

	a := App{
		viewState: MainView,
		tabs: []Tab{
			{Type: SSHTab, Title: "first", Model: tab},
		},
	}

	updated, _ := a.Update(imagePasteFallbackMsg{
		streamID: tab.StreamID(),
		msg:      tea.PasteMsg{Content: "hello"},
	})
	a = updated.(App)

	if got := stdin.String(); got != "hello" {
		t.Fatalf("stdin = %q", got)
	}
}
