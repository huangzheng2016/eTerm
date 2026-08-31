package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/voice"
)

type fakeVoiceEngine struct {
	events  chan voice.Event
	started bool
	stopped bool
	vad     voice.VADParams
	closed  bool
}

func (f *fakeVoiceEngine) Start(context.Context) error    { f.started = true; return nil }
func (f *fakeVoiceEngine) Stop() error                    { f.stopped = true; return nil }
func (f *fakeVoiceEngine) SetVAD(p voice.VADParams) error { f.vad = p; return nil }
func (f *fakeVoiceEngine) Events() <-chan voice.Event     { return f.events }
func (f *fakeVoiceEngine) Close() error                   { f.closed = true; close(f.events); return nil }

// collectCmdMsgs runs cmd and its (flattened) sub-commands, gathering the
// messages they produce until want matches or the deadline passes. Pump
// commands that block on channels keep their goroutines.
func collectCmdMsgs(t *testing.T, cmd tea.Cmd, want func(tea.Msg) bool) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	out := make(chan tea.Msg, 16)
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				go run(sub)
			}
		case nil:
		default:
			out <- msg
		}
	}
	go run(cmd)
	var msgs []tea.Msg
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-out:
			msgs = append(msgs, msg)
			if want != nil && want(msg) {
				return msgs
			}
		case <-deadline:
			return msgs
		}
	}
}

func voiceTestApp(fe *fakeVoiceEngine) App {
	return voiceTestAppMake(fe)
}

func voiceTestAppMake(eng voice.Engine) App {
	return App{
		viewState:      MainView,
		kbConfig:       DefaultKeyBindingConfig(),
		keyMap:         BuildKeyMap(DefaultKeyBindingConfig()),
		voiceCfgLoaded: true,
		voiceCfg:       defaultVoiceSettings(),
		voiceMake: func(voiceSettings, func(float64)) (voice.Engine, error) {
			return eng, nil
		},
	}
}

func TestVoiceHotkeyToggleStartsAndStops(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	key := tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl})

	upd, cmd := a.Update(key)
	a = upd.(App)
	if !a.voiceRec {
		t.Fatal("first press did not start recording")
	}
	msgs := collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	})
	if !fe.started {
		t.Fatal("engine not started")
	}
	// The runtime feeds completions back; they clear voiceBusy.
	for _, m := range msgs {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if a.voiceBusy {
		t.Fatal("voiceBusy stuck after start completed")
	}

	upd, cmd = a.Update(key)
	a = upd.(App)
	if a.voiceRec {
		t.Fatal("second press did not stop recording")
	}
	if cmd == nil {
		t.Fatal("expected stop command")
	}
	if msg := cmd(); msg != nil {
		upd, _ = a.Update(msg)
		a = upd.(App)
	}
	if !fe.stopped {
		t.Fatal("engine not stopped")
	}
	if a.voiceBusy {
		t.Fatal("voiceBusy stuck after stop completed")
	}
}

func TestVoiceHotkeyWorksWithAIOverlayOpen(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	fake := aiview.NewFakeRunner()
	av := aiview.New(fake, fake, fake)
	av.SetSize(80, 24)
	a.aiView = av
	a.aiVisible = true

	upd, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if !a.voiceRec {
		t.Fatal("hotkey did not reach the app while the overlay was open")
	}
	if !strings.Contains(av.View().Content, "REC") {
		t.Fatal("overlay title missing REC indicator")
	}
}

func TestVoiceStatusHintShowsRecording(t *testing.T) {
	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	if got := a.withVoiceStatusHint("hint"); got != "hint" {
		t.Fatalf("idle hint = %q", got)
	}
	a.voiceRec = true
	a.voiceStartedAt = time.Now().Add(-5 * time.Second)
	a.voiceName = voiceEngineLocal
	a.voicePartial = "hello wor"
	got := a.withVoiceStatusHint("hint")
	if !strings.Contains(got, "REC 5s") || !strings.Contains(got, "local") || !strings.Contains(got, "hello wor") {
		t.Fatalf("recording hint = %q", got)
	}
}

func voiceFinalMsg(text string) voiceEventMsg {
	return voiceEventMsg{ev: voice.Event{Type: voice.EventFinal, Text: text}}
}

func TestVoiceDeliveryToAiviewInsertsText(t *testing.T) {
	fake := aiview.NewFakeRunner()
	fake.Delay = 0
	av := aiview.New(fake, fake, fake)
	av.SetSize(80, 24)

	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.aiView = av
	a.aiVisible = true
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	a.voiceCfg.SentenceEnd = voice.SentenceEndSpace

	if _, cmd := a.Update(voiceFinalMsg("hello world")); cmd != nil {
		cmd()
	}
	if !strings.Contains(av.View().Content, "hello world ") {
		t.Fatal("dictated text missing from the AI input")
	}
	if sink.String() != "" {
		t.Fatalf("terminal received text while the overlay was open: %q", sink.String())
	}
}

func TestVoiceDeliveryToAiviewEnterSubmits(t *testing.T) {
	fake := aiview.NewFakeRunner()
	fake.Delay = 0
	av := aiview.New(fake, fake, fake)
	av.SetSize(80, 24)

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.aiView = av
	a.aiVisible = true
	a.voiceCfg.SentenceEnd = voice.SentenceEndEnter

	if _, cmd := a.Update(voiceFinalMsg("hello world")); cmd != nil {
		cmd()
	}
	if !strings.Contains(av.View().Content, "You: hello world") {
		t.Fatal("sentence-end enter did not submit the AI input")
	}
}

func TestVoiceDeliveryToTerminalPasteText(t *testing.T) {
	for _, tc := range []struct {
		end  voice.SentenceEnd
		want string
	}{
		{voice.SentenceEndEnter, "ls -la\n"},
		{voice.SentenceEndSpace, "ls -la "},
	} {
		sink := &syncWriteCloser{}
		is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
		sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))

		a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
		a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
		a.voiceCfg.SentenceEnd = tc.end

		if _, cmd := a.Update(voiceFinalMsg("ls -la")); cmd != nil {
			cmd()
		}
		deadline := time.Now().Add(time.Second)
		for sink.String() != tc.want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: pty stdin = %q, want %q", tc.end, sink.String(), tc.want)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestVoiceSettingsPersistenceRoundTrip(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()

	if got := loadVoiceSettings(database, mk); got != defaultVoiceSettings() {
		t.Fatalf("defaults = %+v", got)
	}

	cfg := voiceSettings{
		Engine:           voiceEngineVolcano,
		VADThreshold:     0.35,
		SentenceEnd:      voice.SentenceEndEnter,
		VolcanoAPIKey:    "api-key",
		VolcanoAppKey:    "app-key",
		VolcanoAccessKey: "access-key",
	}
	if err := persistVoiceSettings(database, mk, cfg); err != nil {
		t.Fatal(err)
	}
	got := loadVoiceSettings(database, mk)
	// Volcano is gated (no audio passthrough yet): the engine coerces to
	// local while the rest round-trips.
	want := cfg
	want.Engine = voiceEngineLocal
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestVoiceSettingsOverlayAdjustAndPersist(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())

	// Engine row is gated while Volcano has no audio passthrough: no change,
	// no persist, and the note is visible.
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if cmd != nil {
		t.Fatal("gated engine row produced a command")
	}
	if m.cfg.Engine != voiceEngineLocal {
		t.Fatalf("engine changed to %q", m.cfg.Engine)
	}
	if !strings.Contains(m.View(), "volcano engine: coming soon") {
		t.Fatal("gated note missing from the view")
	}

	// Threshold row: right steps 0 -> 0.05 and keeps the engine alive.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.VADThreshold != 0.05 || !msg.keepEngine {
		t.Fatalf("threshold change msg = %#v", msg)
	}

	// API key row: enter edits, enter commits, stored encrypted.
	m.cursor = voiceRowAPIKey
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.edit != voiceRowAPIKey {
		t.Fatal("enter did not start editing")
	}
	m.input.SetValue("secret")
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.VolcanoAPIKey != "secret" {
		t.Fatalf("key change msg = %#v", msg)
	}
	if got := loadVoiceSettings(database, mk); got.VolcanoAPIKey != "secret" {
		t.Fatalf("persisted api key = %q", got.VolcanoAPIKey)
	}

	closed, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if !closed {
		t.Fatal("esc did not close the overlay")
	}
}

func TestVoiceHotkeySkippedInSettingsTab(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.masterKey = security.NewMasterKeyManager(nil, nil, time.Minute)
	a.tabs = []Tab{{Type: SettingsTab, Title: "Settings", Model: nil}}
	a.activeTab = 0

	upd, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if a.voiceRec {
		t.Fatal("voice hotkey fired inside the Settings tab")
	}
	if a.voiceEngine != nil {
		t.Fatal("engine was built")
	}
}

func TestVoiceSettingsOverlayMouse(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.masterKey = mk
	a.width = 80
	a.height = 24
	a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: nil}}
	a.voiceSettingsView = newVoiceSettingsModel(database, mk, defaultVoiceSettings())

	// Click the threshold row: cursor moves there and the row adjusts.
	ox, oy, _, _ := a.overlayBounds(a.voiceSettingsView.View())
	click := tea.MouseClickMsg(tea.Mouse{X: ox + 3, Y: oy + 4 + voiceRowThreshold, Button: tea.MouseLeft})
	upd, _ := a.Update(click)
	a = upd.(App)
	if a.voiceSettingsView == nil || a.voiceSettingsView.cursor != voiceRowThreshold {
		t.Fatal("click did not reach the overlay")
	}
	if a.voiceSettingsView.cfg.VADThreshold != 0.05 {
		t.Fatalf("threshold = %v", a.voiceSettingsView.cfg.VADThreshold)
	}

	// Click outside dismisses the overlay without reaching the tab bar.
	upd, _ = a.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
	a = upd.(App)
	if a.voiceSettingsView != nil {
		t.Fatal("outside click did not dismiss the overlay")
	}
	if a.activeTab != 0 {
		t.Fatal("click fell through to the tab bar")
	}
}

func TestVoiceDeliveryDroppedWhenLocked(t *testing.T) {
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.viewState = LoginView
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	a.voiceCfg.SentenceEnd = voice.SentenceEndEnter

	if _, cmd := a.Update(voiceFinalMsg("ls")); cmd != nil {
		cmd()
	}
	time.Sleep(50 * time.Millisecond)
	if sink.String() != "" {
		t.Fatalf("delivered while locked: %q", sink.String())
	}
}

// gateVoiceEngine blocks Start/Stop on gates so tests can force slow engine
// operations (the first-use download is the real-world case).
type gateVoiceEngine struct {
	events    chan voice.Event
	startGate chan struct{}
	stopGate  chan struct{}

	mu     sync.Mutex
	starts int
	stops  int
}

func (g *gateVoiceEngine) Start(context.Context) error {
	if g.startGate != nil {
		<-g.startGate
	}
	g.mu.Lock()
	g.starts++
	g.mu.Unlock()
	return nil
}

func (g *gateVoiceEngine) Stop() error {
	if g.stopGate != nil {
		<-g.stopGate
	}
	g.mu.Lock()
	g.stops++
	g.mu.Unlock()
	return nil
}

func (g *gateVoiceEngine) SetVAD(voice.VADParams) error { return nil }
func (g *gateVoiceEngine) Events() <-chan voice.Event   { return g.events }
func (g *gateVoiceEngine) Close() error                 { return nil }

func (g *gateVoiceEngine) counts() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.starts, g.stops
}

// Stop requested while a slow Start is in flight must land after it.
func TestVoiceToggleReconcileStopAfterSlowStart(t *testing.T) {
	ge := &gateVoiceEngine{events: make(chan voice.Event), startGate: make(chan struct{})}
	a := voiceTestAppMake(ge)

	upd, cmd := a.toggleVoice()
	a = upd
	if !a.voiceRec || !a.voiceBusy {
		t.Fatalf("rec=%v busy=%v", a.voiceRec, a.voiceBusy)
	}
	startMsgs := make(chan []tea.Msg, 1)
	go func(c tea.Cmd) {
		startMsgs <- collectCmdMsgs(t, c, func(m tea.Msg) bool {
			_, ok := m.(voiceStartedMsg)
			return ok
		})
	}(cmd)

	// Toggle off while the start is blocked: intent flips, no stop cmd yet.
	upd, cmd = a.toggleVoice()
	a = upd
	if a.voiceRec || cmd != nil {
		t.Fatalf("rec=%v cmd=%v", a.voiceRec, cmd != nil)
	}

	// Release the start; its completion must reconcile into a stop.
	close(ge.startGate)
	var stopCmd tea.Cmd
	for _, m := range <-startMsgs {
		if _, ok := m.(voiceStartedMsg); !ok {
			continue
		}
		upd2, c := a.Update(m)
		a = upd2.(App)
		stopCmd = c
	}
	if stopCmd == nil {
		t.Fatal("stale start completion did not issue a stop")
	}
	upd2, _ := a.Update(stopCmd())
	a = upd2.(App)

	starts, stops := ge.counts()
	if starts != 1 || stops != 1 {
		t.Fatalf("starts=%d stops=%d", starts, stops)
	}
	if a.voiceRec || a.voiceBusy {
		t.Fatalf("final rec=%v busy=%v", a.voiceRec, a.voiceBusy)
	}
}

// Start requested while a slow Stop is in flight must land after it.
func TestVoiceToggleReconcileStartAfterSlowStop(t *testing.T) {
	ge := &gateVoiceEngine{events: make(chan voice.Event), stopGate: make(chan struct{})}
	a := voiceTestAppMake(ge)

	// Start and let it complete.
	upd, cmd := a.toggleVoice()
	a = upd
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}

	// Toggle off: the stop blocks on the gate.
	upd, cmd = a.toggleVoice()
	a = upd
	stopMsgs := make(chan []tea.Msg, 1)
	go func(c tea.Cmd) {
		stopMsgs <- collectCmdMsgs(t, c, func(m tea.Msg) bool {
			_, ok := m.(voiceStoppedMsg)
			return ok
		})
	}(cmd)

	// Toggle back on while the stop is blocked: intent flips, no start cmd.
	upd, cmd = a.toggleVoice()
	a = upd
	if !a.voiceRec || cmd != nil {
		t.Fatalf("rec=%v cmd=%v", a.voiceRec, cmd != nil)
	}

	// Release the stop; its completion must reconcile into a start.
	close(ge.stopGate)
	var startCmd tea.Cmd
	for _, m := range <-stopMsgs {
		if _, ok := m.(voiceStoppedMsg); !ok {
			continue
		}
		upd2, c := a.Update(m)
		a = upd2.(App)
		startCmd = c
	}
	if startCmd == nil {
		t.Fatal("stale stop completion did not issue a start")
	}
	for _, m := range collectCmdMsgs(t, startCmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}

	starts, stops := ge.counts()
	if starts != 2 || stops != 1 {
		t.Fatalf("starts=%d stops=%d", starts, stops)
	}
	if !a.voiceRec || a.voiceBusy {
		t.Fatalf("final rec=%v busy=%v", a.voiceRec, a.voiceBusy)
	}
}
