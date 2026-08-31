package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	events    chan voice.Event
	started   bool
	stopped   bool
	vad       voice.VADParams
	closed    bool
	modelDir  string
	modelKind string
}

// findVoiceRow returns the index of the first row of kind, -1 when absent.
func findVoiceRow(m *voiceSettingsModel, kind int) int {
	for i, r := range m.rows() {
		if r.kind == kind {
			return i
		}
	}
	return -1
}

// findVoiceParamRow returns the index of the param row for key, -1 when absent.
func findVoiceParamRow(m *voiceSettingsModel, key string) int {
	for i, r := range m.rows() {
		if r.kind == vrowParam && r.param.Key == key {
			return i
		}
	}
	return -1
}

func (f *fakeVoiceEngine) Start(context.Context) error { f.started = true; return nil }
func (f *fakeVoiceEngine) Stop() error                 { f.stopped = true; return nil }
func (f *fakeVoiceEngine) SetVAD(p voice.VADParams) error {
	f.vad = p
	return nil
}
func (f *fakeVoiceEngine) SetModel(dir, kind string) error {
	f.modelDir = dir
	f.modelKind = kind
	return nil
}
func (f *fakeVoiceEngine) Events() <-chan voice.Event { return f.events }
func (f *fakeVoiceEngine) Close() error               { f.closed = true; close(f.events); return nil }

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
		voiceReady:     func(voiceSettings) bool { return true },
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

	if got := loadVoiceSettings(database, mk); !reflect.DeepEqual(got, defaultVoiceSettings()) {
		t.Fatalf("defaults = %+v", got)
	}

	cfg := defaultVoiceSettings()
	cfg.Engine = voiceEngineVolcano
	cfg.VADThreshold = 0.35
	cfg.VADSilenceMs = 1250
	cfg.SentenceEnd = voice.SentenceEndEnter
	cfg.ModelID = voice.ModelCatalog()[2].ID
	cfg.CustomModelDir = "/tmp/custom-model"
	cfg.Verified = true
	cfg.setEngineParam(voiceEngineVolcano, "api_key", "api-key")
	cfg.setEngineParam(voiceEngineVolcano, "app_key", "app-key")
	cfg.setEngineParam(voiceEngineVolcano, "access_key", "access-key")
	if err := persistVoiceSettings(database, mk, cfg); err != nil {
		t.Fatal(err)
	}
	if got := loadVoiceSettings(database, mk); !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}

	// the params blob is encrypted at rest
	blob, err := db.GetSetting(database, voiceParamsSettingPrefix+voiceEngineVolcano)
	if err != nil || blob == "" {
		t.Fatal("params blob not stored")
	}
	if strings.Contains(blob, "api-key") {
		t.Fatal("params stored in plaintext")
	}
}

// The legacy voice_volcano blob migrates into voice_params_volcano on load;
// the old key is deleted afterwards.
func TestVoiceSettingsMigratesLegacyVolcanoKey(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()

	data, err := json.Marshal(map[string]string{"api_key": "a", "app_key": "b", "access_key": "c"})
	if err != nil {
		t.Fatal(err)
	}
	k := mk.GetKey()
	enc, err := security.Encrypt(data, k.Bytes())
	k.Clear()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(database, voiceVolcanoSettingKey, enc); err != nil {
		t.Fatal(err)
	}

	cfg := loadVoiceSettings(database, mk)
	params := cfg.engineParams(voiceEngineVolcano)
	if params["api_key"] != "a" || params["app_key"] != "b" || params["access_key"] != "c" {
		t.Fatalf("migrated params = %v", params)
	}
	if _, err := db.GetSetting(database, voiceVolcanoSettingKey); err == nil {
		t.Fatal("legacy key not deleted")
	}
	blob, err := db.GetSetting(database, voiceParamsSettingPrefix+voiceEngineVolcano)
	if err != nil || blob == "" {
		t.Fatal("new params blob missing")
	}
	if strings.Contains(blob, "api_key") {
		t.Fatal("migrated blob stored in plaintext")
	}

	// the migration ran once; the new blob loads on its own
	cfg = loadVoiceSettings(database, mk)
	if got := cfg.engineParams(voiceEngineVolcano); got["api_key"] != "a" {
		t.Fatalf("reloaded params = %v", got)
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

	// Engine row: right cycles local -> volcano and rebuilds the engine.
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.Engine != voiceEngineVolcano || msg.keepEngine {
		t.Fatalf("engine change msg = %#v", msg)
	}
	if strings.Contains(m.View(), "coming soon") {
		t.Fatal("gated note still in the view")
	}

	// Sensitivity row: right steps 0 -> 0.05 and keeps the engine alive.
	m.cursor = findVoiceRow(m, vrowThreshold)
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.VADThreshold != 0.05 || !msg.keepEngine {
		t.Fatalf("threshold change msg = %#v", msg)
	}

	// Silence row: right steps 1000 -> 1050 and keeps the engine alive.
	m.cursor = findVoiceRow(m, vrowSilence)
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	msg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.VADSilenceMs != 1050 || !msg.keepEngine {
		t.Fatalf("silence change msg = %#v", msg)
	}
	if got := loadVoiceSettings(database, mk); got.VADSilenceMs != 1050 {
		t.Fatalf("persisted silence = %d", got.VADSilenceMs)
	}

	// API key row: enter edits, enter commits, stored encrypted, masked in view.
	m.cursor = findVoiceParamRow(m, "api_key")
	if m.cursor < 0 {
		t.Fatal("api key row missing")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.edit < 0 {
		t.Fatal("enter did not start editing")
	}
	m.input.SetValue("secret")
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.engineParams(voiceEngineVolcano)["api_key"] != "secret" {
		t.Fatalf("key change msg = %#v", msg)
	}
	if got := loadVoiceSettings(database, mk); got.engineParams(voiceEngineVolcano)["api_key"] != "secret" {
		t.Fatalf("persisted api key = %q", got.engineParams(voiceEngineVolcano)["api_key"])
	}
	if view := m.View(); strings.Contains(view, "secret") || !strings.Contains(view, "(set)") {
		t.Fatalf("secret not masked:\n%s", view)
	}

	closed, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if !closed {
		t.Fatal("esc did not close the overlay")
	}
}

// Rows render per the selected engine: local shows the helper and Model
// rows, volcano shows its three key rows, unknown engines render generically.
func TestVoiceSettingsEngineConditionalRows(t *testing.T) {
	m := newVoiceSettingsModel(nil, nil, defaultVoiceSettings())

	// local: helper + Model > rows, no engine param rows
	if findVoiceRow(m, vrowHelper) < 0 || findVoiceRow(m, vrowModels) < 0 {
		t.Fatal("local helper/model rows missing")
	}
	if findVoiceRow(m, vrowParam) >= 0 {
		t.Fatal("local shows engine param rows")
	}
	if view := m.View(); !strings.Contains(view, "Helper binary") || !strings.Contains(view, "Model >") {
		t.Fatalf("local rows not rendered:\n%s", view)
	}

	// volcano: 3 secret param rows, no helper/model rows
	m.cfg.Engine = voiceEngineVolcano
	if findVoiceRow(m, vrowHelper) >= 0 || findVoiceRow(m, vrowModels) >= 0 {
		t.Fatal("volcano shows local-only rows")
	}
	n := 0
	for _, r := range m.rows() {
		if r.kind == vrowParam {
			n++
			if !r.param.Secret {
				t.Fatalf("volcano param %q not secret", r.param.Key)
			}
		}
	}
	if n != 3 {
		t.Fatalf("volcano param rows = %d", n)
	}
	view := m.View()
	for _, label := range []string{"Volcano API key", "Volcano App key", "Volcano Access key"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %q:\n%s", label, view)
		}
	}

	// unknown engine: engine row + shared rows only, labeled unknown
	m.cfg.Engine = "mystery"
	if findVoiceRow(m, vrowEngine) < 0 || findVoiceRow(m, vrowTest) < 0 {
		t.Fatal("unknown engine lost shared rows")
	}
	if findVoiceRow(m, vrowParam) >= 0 || findVoiceRow(m, vrowHelper) >= 0 || findVoiceRow(m, vrowModels) >= 0 {
		t.Fatal("unknown engine shows engine-specific rows")
	}
	if view = m.View(); !strings.Contains(view, "mystery (unknown)") {
		t.Fatalf("unknown engine not labeled:\n%s", view)
	}
	if !strings.Contains(m.View(), "setup incomplete") {
		t.Fatal("unknown engine shown ready")
	}
}

// defaultVoiceEngine picks the local engine or the volcano feed composition
// from the configured engine.
func TestDefaultVoiceEngineSelection(t *testing.T) {
	local, err := defaultVoiceEngine(defaultVoiceSettings(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := local.(*voice.LocalEngine); !ok {
		t.Fatalf("local engine = %T", local)
	}
	local.Close()

	cfg := defaultVoiceSettings()
	cfg.Engine = voiceEngineVolcano
	volc, err := defaultVoiceEngine(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := volc.(*voice.VolcanoFeedEngine); !ok {
		t.Fatalf("volcano engine = %T", volc)
	}
	volc.Close()

	cfg.Engine = "no-such-engine"
	if _, err := defaultVoiceEngine(cfg, nil); err == nil {
		t.Fatal("unknown engine built without error")
	}
}

// A keepEngine settings change applies VAD params live via SetVAD.
func TestVoiceSettingsLiveApplySendsVADParams(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceEngine = fe

	cfg := defaultVoiceSettings()
	cfg.VADThreshold = 0.4
	cfg.VADSilenceMs = 1500
	upd, _ := a.Update(voiceSettingsChangedMsg{cfg: cfg, keepEngine: true})
	a = upd.(App)
	if fe.vad.Threshold != 0.4 || fe.vad.TrailingSilence != 1.5 {
		t.Fatalf("vad = %+v", fe.vad)
	}
	if a.voiceEngine == nil {
		t.Fatal("engine was rebuilt on a keepEngine change")
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
	thresholdRow := findVoiceRow(a.voiceSettingsView, vrowThreshold)
	if thresholdRow < 0 {
		t.Fatal("threshold row missing")
	}
	ox, oy, _, _ := a.overlayBounds(a.voiceSettingsView.View())
	click := tea.MouseClickMsg(tea.Mouse{X: ox + 3, Y: oy + 4 + thresholdRow, Button: tea.MouseLeft})
	upd, _ := a.Update(click)
	a = upd.(App)
	if a.voiceSettingsView == nil || a.voiceSettingsView.cursor != thresholdRow {
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

// With the ctrl+r routing notice visible the panel grows by two lines; row
// clicks must still land on the right row.
func TestVoiceSettingsMouseWithNotice(t *testing.T) {
	stubHelperInstalled(t, false)
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.db = database
	a.masterKey = mk
	a.width = 80
	a.height = 24
	a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: nil}}
	a.voiceReady = func(voiceSettings) bool { return false }

	upd, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if a.voiceSettingsView == nil || a.voiceSettingsView.noticeText() == "" {
		t.Fatal("routing notice missing")
	}

	thresholdRow := findVoiceRow(a.voiceSettingsView, vrowThreshold)
	ox, oy, _, _ := a.overlayBounds(a.voiceSettingsView.View())
	click := tea.MouseClickMsg(tea.Mouse{X: ox + 3, Y: oy + 6 + thresholdRow, Button: tea.MouseLeft})
	upd, _ = a.Update(click)
	a = upd.(App)
	if a.voiceSettingsView == nil || a.voiceSettingsView.cursor != thresholdRow {
		t.Fatal("click did not reach the row below the notice")
	}
	if a.voiceSettingsView.cfg.VADThreshold != 0.05 {
		t.Fatalf("threshold = %v", a.voiceSettingsView.cfg.VADThreshold)
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

func (g *gateVoiceEngine) SetVAD(voice.VADParams) error  { return nil }
func (g *gateVoiceEngine) SetModel(string, string) error { return nil }
func (g *gateVoiceEngine) Events() <-chan voice.Event    { return g.events }
func (g *gateVoiceEngine) Close() error                  { return nil }

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

func stubHelperInstalled(t *testing.T, installed bool) {
	t.Helper()
	old := helperInstalledFn
	helperInstalledFn = func() bool { return installed }
	t.Cleanup(func() { helperInstalledFn = old })
}

func writeFakeModel(t *testing.T, root string, spec voice.ModelSpec) {
	t.Helper()
	dir := spec.ModelDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, spec.File), []byte("x"), 0o644)
}

func TestVoiceSetupReady(t *testing.T) {
	root := t.TempDir()
	cfg := defaultVoiceSettings()

	stubHelperInstalled(t, false)
	if voiceSetupReady(cfg, root) {
		t.Fatal("ready without helper")
	}

	helperInstalledFn = func() bool { return true }
	if voiceSetupReady(cfg, root) {
		t.Fatal("ready without model")
	}

	writeFakeModel(t, root, voice.ModelByID(cfg.ModelID))
	if !voiceSetupReady(cfg, root) {
		t.Fatal("not ready with helper+model")
	}

	vcfg := defaultVoiceSettings()
	vcfg.Engine = voiceEngineVolcano
	if voiceSetupReady(vcfg, root) {
		t.Fatal("volcano ready without keys")
	}
	vcfg.setEngineParam(voiceEngineVolcano, "api_key", "a")
	vcfg.setEngineParam(voiceEngineVolcano, "app_key", "b")
	vcfg.setEngineParam(voiceEngineVolcano, "access_key", "c")
	if !voiceSetupReady(vcfg, root) {
		t.Fatal("volcano not ready with keys")
	}

	ucfg := defaultVoiceSettings()
	ucfg.Engine = "no-such-engine"
	if voiceSetupReady(ucfg, root) {
		t.Fatal("unknown engine ready")
	}

	// a valid custom model dir counts as an installed model
	ccfg := defaultVoiceSettings()
	custom := t.TempDir()
	ccfg.CustomModelDir = custom
	if voiceSetupReady(ccfg, root) {
		t.Fatal("ready with an invalid custom dir")
	}
	os.WriteFile(filepath.Join(custom, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(custom, "model.int8.onnx"), []byte("x"), 0o644)
	if !voiceSetupReady(ccfg, root) {
		t.Fatal("not ready with a valid custom dir")
	}
}

// ctrl+r with an incomplete setup opens the settings panel with guidance
// instead of recording (and failing).
func TestVoiceHotkeyOpensSettingsWhenNotReady(t *testing.T) {
	stubHelperInstalled(t, false)
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceReady = func(voiceSettings) bool { return false }

	upd, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if a.voiceRec {
		t.Fatal("recording started without a complete setup")
	}
	if cmd != nil {
		t.Fatal("unexpected command")
	}
	if a.voiceEngine != nil {
		t.Fatal("engine was built")
	}
	if a.voiceSettingsView == nil {
		t.Fatal("settings panel did not open")
	}
	view := a.voiceSettingsView.View()
	if !strings.Contains(view, "setup incomplete") {
		t.Fatalf("no guidance in panel: %s", view)
	}
	if !strings.Contains(view, "not set up yet") || !strings.Contains(view, "helper binary") {
		t.Fatalf("no routing reason in panel: %s", view)
	}
}

// The routing notice names the engine-specific missing piece.
func TestVoiceHotkeyNoticeNamesMissingKeys(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceReady = func(voiceSettings) bool { return false }
	a.voiceCfg.Engine = voiceEngineVolcano

	upd, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if a.voiceSettingsView == nil {
		t.Fatal("settings panel did not open")
	}
	view := a.voiceSettingsView.View()
	if !strings.Contains(view, "not set up yet") || !strings.Contains(view, "Volcano API key") {
		t.Fatalf("no key guidance in panel: %s", view)
	}
}

func TestVoiceSettingsModelSubmenu(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.modelsRoot = t.TempDir()
	m.helperInstalledFn = func() bool { return false }
	m.refreshInstallState()

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})

	// helper row requests a helper download
	m.cursor = findVoiceRow(m, vrowHelper)
	_, cmd := m.Update(enter)
	req, ok := cmd().(voiceDownloadRequestMsg)
	if !ok || req.target != voiceHelperTarget {
		t.Fatalf("helper download request = %#v", cmd())
	}

	// Model > enters the submenu: back row + 3 catalog rows + custom path row
	m.cursor = findVoiceRow(m, vrowModels)
	m.Update(enter)
	if m.view != voiceViewModels {
		t.Fatal("Model > did not enter the submenu")
	}
	if rows := m.rows(); len(rows) != 5 || rows[0].kind != vrowBack || rows[4].kind != vrowCustomPath {
		t.Fatalf("submenu rows = %+v", rows)
	}

	// not-downloaded model row requests the model download
	m.cursor = 3
	_, cmd = m.Update(enter)
	req, ok = cmd().(voiceDownloadRequestMsg)
	if !ok || req.target != voice.ModelCatalog()[2].ID {
		t.Fatalf("model download request = %#v", cmd())
	}

	// installed model row selects and persists the model, clearing verified
	spec := voice.ModelCatalog()[2]
	writeFakeModel(t, m.modelsRoot, spec)
	m.cfg.Verified = true
	m.refreshInstallState()
	_, cmd = m.Update(enter)
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.ModelID != spec.ID || !chg.keepEngine {
		t.Fatalf("model select msg = %#v", chg)
	}
	if chg.cfg.Verified {
		t.Fatal("model switch must clear verified")
	}
	if got := loadVoiceSettings(database, mk); got.ModelID != spec.ID {
		t.Fatalf("persisted model = %q", got.ModelID)
	}
	if !strings.Contains(m.View(), "[active]") {
		t.Fatal("active model not marked")
	}

	// progress and failures render on the row and stay visible
	m.downloadStarted(spec.ID)
	m.downloadUpdate(voiceDownloadMsg{target: spec.ID, pct: 42})
	if !strings.Contains(m.View(), "downloading 42%") {
		t.Fatal("download progress not rendered")
	}
	m.downloadUpdate(voiceDownloadMsg{target: spec.ID, err: errTest, done: true})
	if !strings.Contains(m.View(), "failed: boom") {
		t.Fatalf("download error not rendered:\n%s", m.View())
	}
	if m.dlTarget != "" {
		t.Fatal("download not cleared after done")
	}

	// left returns to the main panel; re-enter, then esc returns too, and a
	// second esc closes the overlay
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if m.view != voiceViewMain {
		t.Fatal("left did not leave the submenu")
	}
	m.cursor = findVoiceRow(m, vrowModels)
	m.Update(enter)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.view != voiceViewMain {
		t.Fatal("esc did not leave the submenu")
	}
	closed, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if !closed {
		t.Fatal("esc did not close the overlay")
	}
}

// The custom model path entry validates the directory, persists the setting,
// applies via set_model kind sensevoice, and counts toward readiness.
func TestVoiceSettingsCustomModelPath(t *testing.T) {
	stubHelperInstalled(t, true)
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.modelsRoot = t.TempDir()
	m.helperInstalledFn = func() bool { return true }
	m.refreshInstallState()

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	m.cursor = findVoiceRow(m, vrowModels)
	m.Update(enter)
	m.cursor = findVoiceRow(m, vrowCustomPath)
	if m.cursor < 0 {
		t.Fatal("custom path row missing")
	}

	// invalid dir: rejected, error rendered, nothing persisted
	m.Update(enter)
	m.input.SetValue(t.TempDir())
	_, cmd := m.Update(enter)
	if cmd != nil {
		t.Fatal("invalid path produced a persist command")
	}
	if m.customErr == "" {
		t.Fatal("no error for an invalid path")
	}
	if m.cfg.CustomModelDir != "" {
		t.Fatal("invalid path stored")
	}
	if !strings.Contains(m.View(), "invalid:") {
		t.Fatalf("error not rendered:\n%s", m.View())
	}

	// valid dir (tokens.txt + model.int8.onnx): accepted and persisted
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "model.int8.onnx"), []byte("x"), 0o644)
	m.cfg.Verified = true
	m.cursor = findVoiceRow(m, vrowCustomPath)
	m.Update(enter)
	m.input.SetValue(dir)
	_, cmd = m.Update(enter)
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.CustomModelDir != dir || !chg.keepEngine {
		t.Fatalf("custom path msg = %#v", chg)
	}
	if chg.cfg.Verified {
		t.Fatal("custom path must clear verified")
	}
	if got := loadVoiceSettings(database, mk); got.CustomModelDir != dir {
		t.Fatalf("persisted custom dir = %q", got.CustomModelDir)
	}

	// readiness counts the custom path; set_model targets it with sensevoice
	if !voiceSetupReady(m.cfg, m.modelsRoot) {
		t.Fatal("custom path not counted as model present")
	}
	gotDir, gotKind := localModelTarget(m.cfg, m.modelsRoot)
	if gotDir != dir || gotKind != voice.ModelKindSenseVoice {
		t.Fatalf("set_model target = %q %q", gotDir, gotKind)
	}
	if !strings.Contains(m.View(), "[active] "+dir) {
		t.Fatal("custom path not marked active")
	}

	// clearing the path falls back to the catalog selection
	m.cursor = findVoiceRow(m, vrowCustomPath)
	m.Update(enter)
	m.input.SetValue("")
	_, cmd = m.Update(enter)
	chg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.CustomModelDir != "" {
		t.Fatalf("clear msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); got.CustomModelDir != "" {
		t.Fatal("clear not persisted")
	}
}

var errTest = errors.New("boom")

// drainVoiceDownload pumps the download wait command chain until the done
// message arrives.
func drainVoiceDownload(t *testing.T, a App, cmd tea.Cmd) App {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		type res struct{ m tea.Msg }
		ch := make(chan res, 1)
		go func() { ch <- res{cmd()} }()
		select {
		case r := <-ch:
			d, ok := r.m.(voiceDownloadMsg)
			if !ok {
				t.Fatalf("unexpected msg %#v", r.m)
			}
			upd, next := a.Update(r.m)
			a = upd.(App)
			if d.done {
				return a
			}
			cmd = next
		case <-deadline:
			t.Fatal("timed out waiting for download done")
		}
	}
}

func TestVoiceDownloadFlow(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	var got string
	a.voiceDownload = func(target string, progress func(float64)) error {
		got = target
		progress(50)
		return nil
	}

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	a.voiceSettingsView.helperInstalledFn = func() bool { return true }

	upd, cmd := a.Update(voiceDownloadRequestMsg{target: voiceHelperTarget})
	a = upd.(App)
	if !a.voiceDlActive {
		t.Fatal("download not marked active")
	}
	a = drainVoiceDownload(t, a, cmd)
	if got != voiceHelperTarget {
		t.Fatalf("download target = %q", got)
	}
	if a.voiceDlActive {
		t.Fatal("still active after done")
	}
	if a.voiceSettingsView.dlTarget != "" {
		t.Fatal("panel download state not cleared")
	}
	if !a.voiceSettingsView.helperOK {
		t.Fatal("helper state not refreshed")
	}

	// a second request while idle works; failure leaves the panel error
	a.voiceDownload = func(string, func(float64)) error { return errTest }
	upd, cmd = a.Update(voiceDownloadRequestMsg{target: voice.ModelCatalog()[0].ID})
	a = upd.(App)
	a = drainVoiceDownload(t, a, cmd)
	if !strings.Contains(a.voiceSettingsView.View(), "failed: boom") {
		t.Fatal("failed download not shown in panel")
	}
}

func TestVoiceTestRecordingFlow(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	a.db = database
	a.masterKey = mk

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)

	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	if !a.voiceTest || !a.voiceBusy {
		t.Fatalf("test not started: test=%v busy=%v", a.voiceTest, a.voiceBusy)
	}
	if !a.voiceSettingsView.testing {
		t.Fatal("panel not in testing state")
	}
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if !fe.started {
		t.Fatal("engine not started")
	}
	if a.voiceBusy {
		t.Fatal("busy stuck after start")
	}
	spec := voice.ModelByID(a.voiceCfg.ModelID)
	if fe.modelKind != spec.Kind || !strings.HasSuffix(fe.modelDir, spec.Dir) {
		t.Fatalf("set_model = %q %q", fe.modelKind, fe.modelDir)
	}

	// info events (helper-side model downloads) do not abort the test
	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventInfo, Msg: "downloading silero_vad.onnx"}})
	a = upd.(App)
	if !a.voiceTest {
		t.Fatal("info event aborted the test")
	}

	// partials route to the panel, not the recording indicator
	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventPartial, Text: "ni hao"}})
	a = upd.(App)
	if a.voiceSettingsView.testText != "ni hao" {
		t.Fatalf("partial = %q", a.voiceSettingsView.testText)
	}
	if a.voicePartial != "" {
		t.Fatal("partial leaked to recording state")
	}

	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}

	upd, cmd = a.Update(voiceFinalMsg("hello test"))
	a = upd.(App)
	if a.voiceTest {
		t.Fatal("test still active after final")
	}
	if !a.voiceCfg.Verified {
		t.Fatal("verified not set")
	}
	if !strings.Contains(a.voiceSettingsView.View(), "hello test") {
		t.Fatal("transcript not shown in panel")
	}
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStoppedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if !fe.stopped {
		t.Fatal("engine not stopped after test")
	}
	if sink.String() != "" {
		t.Fatalf("test transcript delivered to terminal: %q", sink.String())
	}
	if got := loadVoiceSettings(database, mk); !got.Verified {
		t.Fatal("verified not persisted")
	}
}

// A cancelled test (timeout) stops the engine and swallows the flushed final.
func TestVoiceTestTimeoutSwallowsFinal(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}

	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}

	upd, cmd = a.Update(voiceTestTimeoutMsg{seq: a.voiceTestSeq})
	a = upd.(App)
	if a.voiceTest {
		t.Fatal("test still active after timeout")
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

	upd, _ = a.Update(voiceFinalMsg("late transcript"))
	a = upd.(App)
	if sink.String() != "" {
		t.Fatalf("flushed final delivered: %q", sink.String())
	}
	if a.voiceSwallowFinal {
		t.Fatal("swallow flag not cleared")
	}

	// stale timeout for an old seq is ignored
	upd, _ = a.Update(voiceTestTimeoutMsg{seq: a.voiceTestSeq})
	a = upd.(App)
	if a.voiceBusy {
		t.Fatal("stale timeout restarted a stop")
	}
}

// Test blocked on incomplete setup: no engine op, guidance in the panel.
func TestVoiceTestBlockedWhenNotReady(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceReady = func(voiceSettings) bool { return false }

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	if a.voiceTest || cmd != nil {
		t.Fatal("test started without a complete setup")
	}
	if !strings.Contains(a.voiceSettingsView.View(), "setup incomplete") {
		t.Fatal("no guidance shown")
	}
	if fe.started {
		t.Fatal("engine started")
	}
}

// A model change applies to the live engine via set_model.
func TestVoiceSettingsModelChangeAppliesToEngine(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceEngine = fe

	cfg := defaultVoiceSettings()
	cfg.ModelID = voice.ModelCatalog()[2].ID
	upd, _ := a.Update(voiceSettingsChangedMsg{cfg: cfg, keepEngine: true})
	a = upd.(App)
	if fe.modelKind != voice.ModelKindParaformer {
		t.Fatalf("kind = %q", fe.modelKind)
	}
	if !strings.HasSuffix(fe.modelDir, voice.ModelCatalog()[2].Dir) {
		t.Fatalf("dir = %q", fe.modelDir)
	}
}

// Closing the panel mid-test cancels the recording.
func TestVoiceSettingsCloseStopsTest(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}

	upd, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	a = upd.(App)
	if a.voiceSettingsView != nil {
		t.Fatal("esc did not close the panel")
	}
	if a.voiceTest {
		t.Fatal("test still active after close")
	}
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStoppedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if !fe.stopped {
		t.Fatal("engine not stopped after close")
	}
}

// A cancelled test that flushes no final has its swallow flag cleared by the
// trailing state idle, so later dictation finals still deliver.
func TestVoiceTestCancelSwallowClearedOnIdle(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}

	upd, cmd = a.Update(voiceTestTimeoutMsg{seq: a.voiceTestSeq})
	a = upd.(App)
	if msg := cmd(); msg != nil {
		upd, _ = a.Update(msg)
		a = upd.(App)
	}
	if !a.voiceSwallowFinal {
		t.Fatal("cancel did not arm the final swallow")
	}

	// helper reports idle after the stop; the swallow must clear
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}

	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventState, State: voice.StateIdle}})
	a = upd.(App)
	if a.voiceSwallowFinal {
		t.Fatal("state idle did not clear the swallow")
	}

	// a later final (normal post-stop dictation) delivers again; PasteText
	// runs synchronously inside Update (the returned cmd only re-arms the
	// event pump, which would block the test)
	upd, _ = a.Update(voiceFinalMsg("real dictation"))
	a = upd.(App)
	deadline := time.Now().Add(time.Second)
	for sink.String() == "" {
		if time.Now().After(deadline) {
			t.Fatal("legitimate final swallowed")
		}
		time.Sleep(time.Millisecond)
	}
}

// Clicking outside the panel cancels an active test recording (same as esc):
// the engine stops and the flushed final neither delivers nor verifies.
func TestVoiceSettingsOutsideClickStopsTest(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.width = 80
	a.height = 24
	a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: nil}}
	a.masterKey = security.NewMasterKeyManager(nil, nil, time.Minute)

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if !a.voiceTest {
		t.Fatal("test not running")
	}

	upd, cmd = a.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
	a = upd.(App)
	if a.voiceSettingsView != nil {
		t.Fatal("outside click did not dismiss the panel")
	}
	if a.voiceTest {
		t.Fatal("test still active after outside click")
	}
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStoppedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if !fe.stopped {
		t.Fatal("engine not stopped after outside click")
	}

	// the flushed final is swallowed: no delivery, no verified mark
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	upd, _ = a.Update(voiceFinalMsg("abandoned test speech"))
	a = upd.(App)
	if sink.String() != "" {
		t.Fatalf("flushed final delivered: %q", sink.String())
	}
	if a.voiceCfg.Verified {
		t.Fatal("abandoned test marked the setup verified")
	}
}

// Starting a panel test while dictation is active is refused; the dictation
// event stream is not hijacked.
func TestVoiceTestRejectedWhileDictating(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceEngine = fe
	a.voiceRec = true

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	if a.voiceTest {
		t.Fatal("test started during dictation")
	}
	if cmd != nil {
		t.Fatal("unexpected command")
	}
	if !strings.Contains(a.voiceSettingsView.View(), "stop dictation") {
		t.Fatalf("no refusal shown:\n%s", a.voiceSettingsView.View())
	}

	// dictation partials still go to the recording indicator, not the panel
	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventPartial, Text: "dictating"}})
	a = upd.(App)
	if a.voicePartial != "dictating" {
		t.Fatalf("dictation partial hijacked: %q", a.voicePartial)
	}
	if a.voiceSettingsView.testText != "" {
		t.Fatal("partial leaked into the panel")
	}
}
