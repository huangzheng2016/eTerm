package app

import (
	"context"
	"strings"
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
	return App{
		viewState:      MainView,
		kbConfig:       DefaultKeyBindingConfig(),
		keyMap:         BuildKeyMap(DefaultKeyBindingConfig()),
		voiceCfgLoaded: true,
		voiceCfg:       defaultVoiceSettings(),
		voiceMake: func(voiceSettings, func(float64)) (voice.Engine, error) {
			return fe, nil
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
	collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	})
	if !fe.started {
		t.Fatal("engine not started")
	}

	upd, cmd = a.Update(key)
	a = upd.(App)
	if a.voiceRec {
		t.Fatal("second press did not stop recording")
	}
	if cmd == nil {
		t.Fatal("expected stop command")
	}
	cmd()
	if !fe.stopped {
		t.Fatal("engine not stopped")
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
	if got := loadVoiceSettings(database, mk); got != cfg {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
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

	// Engine row: right cycles local -> volcano and persists (engine rebuild).
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.Engine != voiceEngineVolcano || msg.keepEngine {
		t.Fatalf("engine change msg = %#v", msg)
	}
	if got, _ := db.GetSetting(database, voiceEngineSettingKey); got != voiceEngineVolcano {
		t.Fatalf("persisted engine = %q", got)
	}

	// Threshold row: right steps 0 -> 0.05 and keeps the engine alive.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok = cmd().(voiceSettingsChangedMsg)
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
