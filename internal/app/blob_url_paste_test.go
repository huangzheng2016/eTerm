package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/clipboardblob"
	internaldb "github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/syncblob"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
	"gorm.io/gorm"
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

func (w *testWriteCloser) waitString(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := w.String(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if got := w.String(); got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
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

	firstStdin.waitString(t, "[i.png](https://example.test/i.png) ")
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

	stdin.waitString(t, "hello")
}

func TestLocalFilePasteUsesFileURL(t *testing.T) {
	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{
			Data:      []byte("abc"),
			Mime:      "application/gzip",
			Filename:  "a b.tar.gz",
			LocalPath: "/tmp/a b.tar.gz",
		}, nil
	}
	t.Cleanup(func() { readClipboardBlob = oldRead })

	stdin := &testWriteCloser{}
	tab := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "local", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	a := App{
		viewState: MainView,
		activeTab: 0,
		tabs: []Tab{
			{Type: LocalTab, Title: "local", Model: tab},
		},
	}

	ch := make(chan syncblob.Progress, 1)
	msg := uploadImageURLCmd(ch, esync.Config{}, nil, nil, tab.StreamID(), nil, nil, true)()
	updated, _ := a.Update(msg)
	a = updated.(App)

	stdin.waitString(t, "[a b.tar.gz](file:///tmp/a%20b.tar.gz) ")
}

func TestLocalFolderPasteUsesFileURL(t *testing.T) {
	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{
			Filename:  "example",
			LocalPath: "/tmp/example/",
		}, nil
	}
	t.Cleanup(func() { readClipboardBlob = oldRead })

	stdin := &testWriteCloser{}
	tab := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "local", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	a := App{
		viewState: MainView,
		activeTab: 0,
		tabs: []Tab{
			{Type: LocalTab, Title: "local", Model: tab},
		},
	}

	ch := make(chan syncblob.Progress, 1)
	msg := uploadImageURLCmd(ch, esync.Config{}, nil, nil, tab.StreamID(), nil, nil, true)()
	updated, _ := a.Update(msg)
	a = updated.(App)

	stdin.waitString(t, "[example](file:///tmp/example/) ")
}

func TestFolderUploadOnSSHTabFails(t *testing.T) {
	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{
			Filename:  "example",
			LocalPath: "/tmp/example/",
		}, nil
	}
	t.Cleanup(func() { readClipboardBlob = oldRead })

	tab := sshview.New(nil, "remote", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })

	ch := make(chan syncblob.Progress, 1)
	msg := uploadImageURLCmd(ch, esync.Config{}, nil, nil, tab.StreamID(), nil, nil, false)()
	done, ok := msg.(types.ImageUploadDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if done.Err == nil || done.Err.Error() != "clipboard folder cannot be uploaded" {
		t.Fatalf("err = %v", done.Err)
	}
}

func TestSSHModeUploadUsesTunnelAndRemoteLoopbackURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"b1","url":"/b/tok","mime":"image/png","bytes":3}`))
	}))
	defer srv.Close()

	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{Data: []byte("abc"), Mime: "image/png", Filename: "a.png"}, nil
	}
	oldTunnel := openTunnel
	openTunnel = func(database *gorm.DB, mk *security.MasterKeyManager, hostID uint, remotePort int) (string, io.Closer, error) {
		if hostID != 7 || remotePort != 18443 {
			t.Fatalf("hostID=%d remotePort=%d", hostID, remotePort)
		}
		return srv.URL, io.NopCloser(bytes.NewReader(nil)), nil
	}
	t.Cleanup(func() {
		readClipboardBlob = oldRead
		openTunnel = oldTunnel
	})

	tab := sshview.New(nil, "remote", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })

	cfg := esync.Config{Enabled: true, Mode: "ssh", SSHHostID: 7, RemotePort: 18443, APIKey: "key", Passphrase: "p"}
	ch := make(chan syncblob.Progress, 1)
	msg := uploadImageURLCmd(ch, cfg, nil, nil, tab.StreamID(), nil, nil, false)()
	done, ok := msg.(types.ImageUploadDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if done.Err != nil {
		t.Fatal(done.Err)
	}
	if done.URL != "http://127.0.0.1:18443/b/tok" {
		t.Fatalf("url = %q", done.URL)
	}
}

func TestSSHModeUploadRequiresHost(t *testing.T) {
	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{Data: []byte("abc"), Mime: "image/png", Filename: "a.png"}, nil
	}
	t.Cleanup(func() { readClipboardBlob = oldRead })

	tab := sshview.New(nil, "remote", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })

	cfg := esync.Config{Enabled: true, Mode: "ssh", APIKey: "key", Passphrase: "p"}
	ch := make(chan syncblob.Progress, 1)
	msg := uploadImageURLCmd(ch, cfg, nil, nil, tab.StreamID(), nil, nil, false)()
	done, ok := msg.(types.ImageUploadDoneMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if done.Err == nil || done.Err.Error() != "sync is not configured" {
		t.Fatalf("err = %v", done.Err)
	}
}

func TestPasteImageURLMsgForcesUploadForLocalFiles(t *testing.T) {
	a, tab := localClipboardPasteTestApp(t)
	_, cmd := a.Update(types.PasteImageURLMsg{})
	assertForcedPasteNeedsSync(t, cmd, tab.StreamID())
}

func TestPasteImageURLKeyForcesUploadForLocalFiles(t *testing.T) {
	a, tab := localClipboardPasteTestApp(t)
	_, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: 'i', Text: "I", Mod: tea.ModCtrl | tea.ModShift}))
	assertForcedPasteNeedsSync(t, cmd, tab.StreamID())
}

func TestActiveTabIsLocalShellIncludesLocalTmuxAndExcludesRemoteLocal(t *testing.T) {
	localTmux := sshview.New(nil, "tmux", 0, viewkeys.SSHKeys{})
	remoteLocal := sshview.New(nil, "remote", 0, viewkeys.SSHKeys{})
	remoteLocal.SetRemoteReconnect(&types.RemoteReconnect{Peer: types.RemotePeer{ID: "peer"}})
	t.Cleanup(func() { _ = localTmux.Close() })
	t.Cleanup(func() { _ = remoteLocal.Close() })

	a := App{
		viewState: MainView,
		activeTab: 0,
		tabs: []Tab{
			{Type: LocalTab, Title: "[T]work", Model: localTmux, TmuxSession: "work"},
			{Type: LocalTab, Title: "[R]peer", Model: remoteLocal},
		},
	}

	if !a.activeTabIsLocalShell() {
		t.Fatal("local tmux should use local file links")
	}
	a.activeTab = 1
	if a.activeTabIsLocalShell() {
		t.Fatal("remote LocalShell should not use local file links")
	}
}

func localClipboardPasteTestApp(t *testing.T) (App, *sshview.Model) {
	t.Helper()
	oldRead := readClipboardBlob
	readClipboardBlob = func() (*clipboardblob.Blob, error) {
		return &clipboardblob.Blob{
			Data:      []byte("abc"),
			Mime:      "application/gzip",
			Filename:  "a b.tar.gz",
			LocalPath: "/tmp/a b.tar.gz",
		}, nil
	}
	t.Cleanup(func() { readClipboardBlob = oldRead })

	database, err := internaldb.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	tab := sshview.New(&internalssh.InteractiveSession{Stdin: &testWriteCloser{}}, "local", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	cfg := DefaultKeyBindingConfig()
	return App{
		db:        database,
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		viewState: MainView,
		activeTab: 0,
		keyMap:    BuildKeyMap(cfg),
		kbConfig:  cfg,
		tabs: []Tab{
			{Type: LocalTab, Title: "local", Model: tab},
		},
	}, tab
}

func assertForcedPasteNeedsSync(t *testing.T, cmd tea.Cmd, streamID uint64) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd = %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d", len(batch))
	}
	msg := batch[1]()
	done, ok := msg.(types.ImageUploadDoneMsg)
	if !ok {
		t.Fatalf("upload msg = %T", msg)
	}
	if done.StreamID != streamID {
		t.Fatalf("stream id = %d", done.StreamID)
	}
	if done.Err == nil || done.Err.Error() != "sync is not configured" {
		t.Fatalf("err = %v", done.Err)
	}
	if done.URL != "" {
		t.Fatalf("url = %q", done.URL)
	}
}
