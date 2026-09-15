package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
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
	context   string
	contextFn func() string
}

func findVoiceRow(m *voiceSettingsModel, kind int) int {
	for i, r := range m.rows() {
		if r.kind == kind {
			return i
		}
	}
	return -1
}

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
func (f *fakeVoiceEngine) SetContext(ctx string) error {
	f.context = ctx
	return nil
}
func (f *fakeVoiceEngine) SetContextProvider(fn func() string) {
	f.contextFn = fn
}
func (f *fakeVoiceEngine) Events() <-chan voice.Event { return f.events }
func (f *fakeVoiceEngine) Close() error               { f.closed = true; close(f.events); return nil }

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
	if got, want := a.withVoiceStatusHint("hint"), ui.ErrorStyle.Render("REC")+" · hint"; got != want {
		t.Fatalf("recording hint = %q, want %q", got, want)
	}
	bar := components.NewStatusBar().SetWidth(60).SetText(a.withVoiceStatusHint("hint")).View()
	if !strings.Contains(bar, ui.ErrorStyle.Render("REC")) {
		t.Fatalf("status bar missing REC: %q", bar)
	}
}

func TestVoicePartialNeverReachesTerminal(t *testing.T) {
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))

	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.voiceRec = true
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}

	upd, _ := a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventPartial, Text: "hel"}})
	a = upd.(App)
	time.Sleep(50 * time.Millisecond)
	if sink.String() != "" {
		t.Fatalf("partial reached pty: %q", sink.String())
	}
}

func TestVoiceFinalWithoutTerminalToastsOnce(t *testing.T) {
	a := voiceTestApp(&fakeVoiceEngine{events: make(chan voice.Event)})
	a.voiceRec = true

	upd, cmd := a.Update(voiceFinalMsg("ls"))
	a = upd.(App)
	if cmd == nil {
		t.Fatal("expected toast command")
	}
	if !strings.Contains(a.toast.View(), "no active terminal tab") {
		t.Fatalf("toast = %q", a.toast.View())
	}
	if !a.voiceDropNotified {
		t.Fatal("drop not marked notified")
	}

	upd, cmd = a.Update(voiceFinalMsg("ls again"))
	a = upd.(App)
	if cmd != nil {
		t.Fatal("duplicate toast on second drop")
	}

	a2, tcmd := a.toggleVoice()
	a = a2
	if tcmd != nil {
		tcmd()
	}
	a3, _ := a.toggleVoice()
	a = a3
	if !a.voiceRec {
		t.Fatal("second toggle did not restart dictation")
	}
	if a.voiceDropNotified {
		t.Fatal("notified flag not reset on new dictation")
	}
}

func voiceFinalMsg(text string) voiceEventMsg {
	return voiceEventMsg{ev: voice.Event{Type: voice.EventFinal, Text: text}}
}

func TestVoiceNoSpeechTimeoutPerPath(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	key := tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl})
	pump := func(cmd tea.Cmd, want func(tea.Msg) bool) {
		for _, m := range collectCmdMsgs(t, cmd, want) {
			upd, _ := a.Update(m)
			a = upd.(App)
		}
	}
	started := func(m tea.Msg) bool { _, ok := m.(voiceStartedMsg); return ok }
	stopped := func(m tea.Msg) bool { _, ok := m.(voiceStoppedMsg); return ok }

	upd, cmd := a.Update(key)
	a = upd.(App)
	if fe.vad.NoSpeechTimeout != 0 {
		t.Fatalf("dictation no-speech timeout = %v", fe.vad.NoSpeechTimeout)
	}
	pump(cmd, started)

	upd, cmd = a.Update(key)
	a = upd.(App)
	pump(cmd, stopped)

	upd, cmd = a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	if fe.vad.NoSpeechTimeout != voiceTestNoSpeechSecs {
		t.Fatalf("test no-speech timeout = %v", fe.vad.NoSpeechTimeout)
	}
	pump(cmd, started)

	upd, cmd = a.Update(voiceFinalMsg("done"))
	a = upd.(App)
	pump(cmd, stopped)

	upd, _ = a.Update(key)
	a = upd.(App)
	if fe.vad.NoSpeechTimeout != 0 {
		t.Fatalf("dictation no-speech timeout after test = %v", fe.vad.NoSpeechTimeout)
	}
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
	cfg.ModelID = voice.ModelCatalog()[1].ID
	cfg.CustomModelDir = "/tmp/custom-model"
	cfg.Verified = true
	cfg.setEngineParam(voiceEngineVolcano, "api_key", "api-key")
	cfg.setEngineParam(voiceEngineVolcano, "resource_id", voice.ResourceIDBigASRConcurrent)
	if err := persistVoiceSettings(database, mk, cfg); err != nil {
		t.Fatal(err)
	}
	if got := loadVoiceSettings(database, mk); !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}

	blob, err := db.GetSetting(database, voiceParamsSettingPrefix+voiceEngineVolcano)
	if err != nil || blob == "" {
		t.Fatal("params blob not stored")
	}
	if strings.Contains(blob, "api-key") {
		t.Fatal("params stored in plaintext")
	}
}

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
	if params["api_key"] != "a" {
		t.Fatalf("migrated params = %v", params)
	}
	if _, ok := params["app_key"]; ok {
		t.Fatalf("deprecated app_key migrated: %v", params)
	}
	if _, ok := params["access_key"]; ok {
		t.Fatalf("deprecated access_key migrated: %v", params)
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

	cfg = loadVoiceSettings(database, mk)
	if got := cfg.engineParams(voiceEngineVolcano); got["api_key"] != "a" {
		t.Fatalf("reloaded params = %v", got)
	}
}

func TestVoiceSettingsTabStagedAdjustAndSave(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	for i, d := range enginePickerDescriptors() {
		if d.ID == voiceEngineVolcano {
			m.cursor = i
		}
	}
	_, cmd := m.Update(enter)
	if cmd != nil {
		t.Fatal("engine select must be staged, not persisted")
	}
	if m.cfg.Engine != voiceEngineVolcano || !m.modified {
		t.Fatalf("staged engine = %q modified=%v", m.cfg.Engine, m.modified)
	}
	if got := loadVoiceSettings(database, mk); got.Engine != voiceEngineLocal {
		t.Fatal("engine persisted before save")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	msg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || msg.cfg.Engine != voiceEngineVolcano || msg.keepEngine {
		t.Fatalf("save msg = %#v", msg)
	}
	if got := loadVoiceSettings(database, mk); got.Engine != voiceEngineVolcano {
		t.Fatal("save did not persist the engine")
	}
	m.saveDone(msg.cfg)
	if m.modified {
		t.Fatal("modified not cleared after save")
	}

	m.cursor = findVoiceRow(m, vrowThreshold)
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if cmd != nil || m.cfg.VADThreshold != 0.05 || !m.modified {
		t.Fatalf("threshold adjust: cmd=%v threshold=%v modified=%v", cmd, m.cfg.VADThreshold, m.modified)
	}

	m.cursor = findVoiceRow(m, vrowSilence)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.cfg.VADSilenceMs != 1050 {
		t.Fatalf("silence = %d", m.cfg.VADSilenceMs)
	}

	m.cursor = findVoiceParamRow(m, "api_key")
	if m.cursor < 0 {
		t.Fatal("api key row missing")
	}
	m.Update(enter)
	if m.edit < 0 {
		t.Fatal("enter did not start editing")
	}
	m.input.SetValue("secret")
	if view := m.View().Content; strings.Contains(view, "secret") || !strings.Contains(view, "******") {
		t.Fatalf("secret echoed while editing:\n%s", view)
	}
	_, cmd = m.Update(enter)
	if cmd != nil {
		t.Fatal("edit commit must be staged")
	}
	if m.cfg.engineParams(voiceEngineVolcano)["api_key"] != "secret" {
		t.Fatal("staged api key missing")
	}
	if view := m.View().Content; strings.Contains(view, "secret") || !strings.Contains(view, "(set)") {
		t.Fatalf("secret not masked:\n%s", view)
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if m.cfg.VADThreshold != 0 || m.cfg.VADSilenceMs != 1000 || m.cfg.engineParams(voiceEngineVolcano)["api_key"] != "" {
		t.Fatalf("reset did not restore saved values: %+v", m.cfg)
	}
	if m.modified {
		t.Fatal("modified set after reset")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if _, ok := cmd().(types.CloseTabMsg); !ok {
		t.Fatalf("esc cmd = %#v", cmd())
	}
}

func TestVoiceSettingsParamOptionsCycle(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)
	m.cfg.Engine = voiceEngineVolcano

	m.cursor = findVoiceParamRow(m, "resource_id")
	if m.cursor < 0 {
		t.Fatal("resource_id row missing")
	}
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.ResourceIDSeedASR {
		t.Fatalf("default resource_id = %q", got)
	}

	right := tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	left := tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})

	_, cmd := m.Update(right)
	if cmd != nil {
		t.Fatal("options cycle must be staged")
	}
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.VolcanoResourceIDs[1] {
		t.Fatalf("right cycle = %q", got)
	}
	if got := loadVoiceSettings(database, mk).engineParams(voiceEngineVolcano)["resource_id"]; got != voice.ResourceIDSeedASR {
		t.Fatal("cycled value persisted before save")
	}

	m.Update(left)
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.ResourceIDSeedASR {
		t.Fatalf("left cycle = %q", got)
	}

	m.cfg.setEngineParam(voiceEngineVolcano, "resource_id", "bogus")
	m.Update(right)
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.VolcanoResourceIDs[0] {
		t.Fatalf("right from unknown = %q", got)
	}
	m.cfg.setEngineParam(voiceEngineVolcano, "resource_id", "bogus")
	m.Update(left)
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.VolcanoResourceIDs[3] {
		t.Fatalf("left from unknown = %q", got)
	}

	if view := m.View().Content; !strings.Contains(view, voice.VolcanoResourceIDs[3]) {
		t.Fatalf("current value not shown:\n%s", view)
	}

	m.cfg.setEngineParam(voiceEngineVolcano, "resource_id", voice.ResourceIDSeedASR)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := m.cfg.engineParams(voiceEngineVolcano)["resource_id"]; got != voice.VolcanoResourceIDs[1] {
		t.Fatal("enter did not cycle the options param")
	}
	if m.edit >= 0 {
		t.Fatal("enter on an options param started editing")
	}
}

func TestVoiceSettingsEngineConditionalRows(t *testing.T) {
	m := newVoiceSettingsModel(nil, nil, defaultVoiceSettings())
	m.SetSize(80, 30)

	titles := func() string {
		var out []string
		for _, s := range m.sections() {
			out = append(out, s.title)
		}
		return strings.Join(out, "|")
	}

	if got := titles(); got != "Engine|Model|Voice Helper|Input|Microphone test" {
		t.Fatalf("local sections = %q", got)
	}
	if findVoiceRow(m, vrowHelper) < 0 || findVoiceRow(m, vrowModel) < 0 {
		t.Fatal("local helper/model rows missing")
	}
	if findVoiceRow(m, vrowParam) >= 0 {
		t.Fatal("local shows engine param rows")
	}
	if findVoiceRow(m, vrowContext) >= 0 {
		t.Fatal("local shows the volcano feature rows")
	}
	if view := m.View().Content; !strings.Contains(view, "Voice Helper") {
		t.Fatalf("local rows not rendered:\n%s", view)
	}
	if strings.Contains(m.View().Content, "Volcano features") {
		t.Fatal("local rendered the volcano features section")
	}

	m.cfg.Engine = voiceEngineVolcano
	if got := titles(); got != "Engine|Volcano Engine 设置|Input|Microphone test|Volcano features" {
		t.Fatalf("volcano sections = %q", got)
	}
	for _, r := range m.sections()[0].rows {
		if r.kind != vrowEngineOption {
			t.Fatal("engine section holds non-option rows")
		}
	}
	if findVoiceRow(m, vrowHelper) >= 0 || findVoiceRow(m, vrowModel) >= 0 {
		t.Fatal("volcano shows local-only rows")
	}
	if findVoiceRow(m, vrowContext) < 0 || findVoiceRow(m, vrowDDC) < 0 {
		t.Fatal("volcano missing the volcano feature rows")
	}
	n := 0
	for _, r := range m.rows() {
		if r.kind == vrowParam {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("volcano param rows = %d", n)
	}
	view := m.View().Content
	for _, label := range []string{"Volcano API key", "Volcano model", voice.ResourceIDSeedASR, "Volcano features"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %q:\n%s", label, view)
		}
	}
	if strings.Contains(view, "Extra features") {
		t.Fatal("old section title still rendered")
	}

	m.cfg.Engine = "mystery"
	if got := titles(); got != "Engine|Input|Microphone test" {
		t.Fatalf("unknown engine sections = %q", got)
	}
	if findVoiceRow(m, vrowTest) < 0 {
		t.Fatal("unknown engine lost shared rows")
	}
	if findVoiceRow(m, vrowParam) >= 0 || findVoiceRow(m, vrowHelper) >= 0 || findVoiceRow(m, vrowModel) >= 0 || findVoiceRow(m, vrowContext) >= 0 {
		t.Fatal("unknown engine shows engine-specific rows")
	}
	if view = m.View().Content; !strings.Contains(view, "mystery (unknown)") {
		t.Fatalf("unknown engine not labeled:\n%s", view)
	}
	if !strings.Contains(m.View().Content, "setup incomplete") {
		t.Fatal("unknown engine shown ready")
	}
}

func TestVoiceSettingsLongValuesTruncated(t *testing.T) {
	m := newVoiceSettingsModel(nil, nil, defaultVoiceSettings())
	m.SetSize(80, 24)
	m.dlErr = strings.Repeat("x", 300)
	m.dlErrTarget = voiceHelperTarget
	m.cfg.CustomModelDir = "/" + strings.Repeat("long-path-segment/", 30)
	assertWidth := func(view string) {
		t.Helper()
		for _, line := range strings.Split(view, "\n")[1:] {
			if w := lipgloss.Width(line); w > 80 {
				t.Fatalf("line width %d exceeds 80: %q", w, line)
			}
		}
	}
	assertWidth(m.View().Content)

	m.cfg.Engine = voiceEngineVolcano
	m.cfg.setEngineParam(voiceEngineVolcano, "api_key", strings.Repeat("k", 100))
	m.cursor = findVoiceParamRow(m, "api_key")
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.edit < 0 {
		t.Fatal("enter did not start editing")
	}
	assertWidth(m.View().Content)
}

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

func voiceTabLineIndex(m *voiceSettingsModel, row int) int {
	for i, sl := range m.buildScrollLines() {
		if sl.logicalIdx == row {
			return i
		}
	}
	return -1
}

func TestVoiceSettingsTabMouse(t *testing.T) {
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

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	vt := a.voiceTab()
	if vt == nil {
		t.Fatal("voice tab did not open")
	}

	thresholdRow := findVoiceRow(vt, vrowThreshold)
	lineIdx := voiceTabLineIndex(vt, thresholdRow)
	if lineIdx < 0 {
		t.Fatal("threshold row missing")
	}
	top := a.MainViewChromeTopLines()
	click := tea.MouseClickMsg(tea.Mouse{X: 4, Y: top + 2 + lineIdx, Button: tea.MouseLeft})
	upd, _ = a.Update(click)
	a = upd.(App)
	if vt.cursor != thresholdRow {
		t.Fatalf("click cursor = %d, want %d", vt.cursor, thresholdRow)
	}
}

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
	vt := a.voiceTab()
	if vt == nil || vt.noticeText() == "" {
		t.Fatal("routing notice missing")
	}

	thresholdRow := findVoiceRow(vt, vrowThreshold)
	lineIdx := voiceTabLineIndex(vt, thresholdRow)
	top := a.MainViewChromeTopLines()
	click := tea.MouseClickMsg(tea.Mouse{X: 4, Y: top + 2 + lineIdx, Button: tea.MouseLeft})
	upd, _ = a.Update(click)
	a = upd.(App)
	if vt.cursor != thresholdRow {
		t.Fatal("click did not reach the row below the notice")
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
func (g *gateVoiceEngine) SetContext(string) error       { return nil }
func (g *gateVoiceEngine) Events() <-chan voice.Event    { return g.events }
func (g *gateVoiceEngine) Close() error                  { return nil }

func (g *gateVoiceEngine) counts() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.starts, g.stops
}

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

	upd, cmd = a.toggleVoice()
	a = upd
	if a.voiceRec || cmd != nil {
		t.Fatalf("rec=%v cmd=%v", a.voiceRec, cmd != nil)
	}

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

func TestVoiceToggleReconcileStartAfterSlowStop(t *testing.T) {
	ge := &gateVoiceEngine{events: make(chan voice.Event), stopGate: make(chan struct{})}
	a := voiceTestAppMake(ge)

	upd, cmd := a.toggleVoice()
	a = upd
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}

	upd, cmd = a.toggleVoice()
	a = upd
	stopMsgs := make(chan []tea.Msg, 1)
	go func(c tea.Cmd) {
		stopMsgs <- collectCmdMsgs(t, c, func(m tea.Msg) bool {
			_, ok := m.(voiceStoppedMsg)
			return ok
		})
	}(cmd)

	upd, cmd = a.toggleVoice()
	a = upd
	if !a.voiceRec || cmd != nil {
		t.Fatalf("rec=%v cmd=%v", a.voiceRec, cmd != nil)
	}

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
	if !voiceSetupReady(vcfg, root) {
		t.Fatal("volcano not ready with api key")
	}

	ucfg := defaultVoiceSettings()
	ucfg.Engine = "no-such-engine"
	if voiceSetupReady(ucfg, root) {
		t.Fatal("unknown engine ready")
	}

	ccfg := defaultVoiceSettings()
	custom := t.TempDir()
	ccfg.CustomModelDir = custom
	if voiceSetupReady(ccfg, root) {
		t.Fatal("ready with an invalid custom dir")
	}
	if issue := voiceSetupIssue(ccfg, root); !strings.Contains(issue, "custom model path invalid") {
		t.Fatalf("invalid custom dir issue = %q", issue)
	}
	os.WriteFile(filepath.Join(custom, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(custom, "model.int8.onnx"), []byte("x"), 0o644)
	if !voiceSetupReady(ccfg, root) {
		t.Fatal("not ready with a valid custom dir")
	}
}

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
	vt := a.voiceTab()
	if vt == nil {
		t.Fatal("voice tab did not open")
	}
	view := vt.View().Content
	if !strings.Contains(view, "setup incomplete") {
		t.Fatalf("no guidance in tab: %s", view)
	}
	if !strings.Contains(view, "not set up yet") || !strings.Contains(view, "helper binary") {
		t.Fatalf("no routing reason in tab: %s", view)
	}
}

func TestVoiceHotkeyNoticeNamesMissingKeys(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceReady = func(voiceSettings) bool { return false }
	a.voiceCfg.Engine = voiceEngineVolcano

	upd, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	vt := a.voiceTab()
	if vt == nil {
		t.Fatal("voice tab did not open")
	}
	view := vt.View().Content
	if !strings.Contains(view, "not set up yet") || !strings.Contains(view, "Volcano API key") {
		t.Fatalf("no key guidance in tab: %s", view)
	}
}

func TestVoiceSettingsModelRows(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)
	m.modelsRoot = t.TempDir()
	m.helperInstalledFn = func() bool { return false }
	m.refreshInstallState()

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})

	helperIdx := findVoiceRow(m, vrowHelper)
	m.cursor = helperIdx
	_, cmd := m.Update(enter)
	req, ok := cmd().(voiceDownloadRequestMsg)
	if !ok || req.target != voiceHelperTarget {
		t.Fatalf("helper download request = %#v", cmd())
	}

	rows := m.rows()
	modelIdx := findVoiceRow(m, vrowModel)
	if modelIdx < 0 || len(rows) < modelIdx+5 || rows[modelIdx+1].kind != vrowModel || rows[modelIdx+2].kind != vrowCustomPath || rows[modelIdx+3].kind != vrowPrecision || rows[modelIdx+4].kind != vrowHelper {
		t.Fatalf("model rows = %+v", rows)
	}

	m.cursor = modelIdx + 1
	_, cmd = m.Update(enter)
	req, ok = cmd().(voiceDownloadRequestMsg)
	if !ok || req.target != voice.ModelCatalog()[1].ID {
		t.Fatalf("model download request = %#v", cmd())
	}

	spec := voice.ModelCatalog()[1]
	writeFakeModel(t, m.modelsRoot, spec)
	m.cfg.Verified = true
	m.refreshInstallState()
	_, cmd = m.Update(enter)
	if cmd != nil {
		t.Fatal("model select must be staged")
	}
	if m.cfg.ModelID != spec.ID || m.cfg.Verified || !m.modified {
		t.Fatalf("staged model = %q verified=%v modified=%v", m.cfg.ModelID, m.cfg.Verified, m.modified)
	}
	if got := loadVoiceSettings(database, mk); got.ModelID == spec.ID {
		t.Fatal("model persisted before save")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.ModelID != spec.ID || !chg.keepEngine {
		t.Fatalf("save msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); got.ModelID != spec.ID {
		t.Fatalf("persisted model = %q", got.ModelID)
	}
	m.saveDone(chg.cfg)
	if !strings.Contains(m.View().Content, "[active]") {
		t.Fatal("active model not marked")
	}

	m.downloadStarted(spec.ID)
	m.downloadUpdate(voiceDownloadMsg{target: spec.ID, pct: 42})
	if !strings.Contains(m.View().Content, "downloading 42%") {
		t.Fatal("download progress not rendered")
	}
	m.downloadUpdate(voiceDownloadMsg{target: spec.ID, err: errTest, done: true})
	if !strings.Contains(m.View().Content, "failed: boom") {
		t.Fatalf("download error not rendered:\n%s", m.View().Content)
	}
	if m.dlTarget != "" {
		t.Fatal("download not cleared after done")
	}
}

func TestVoiceSettingsCustomModelPath(t *testing.T) {
	stubHelperInstalled(t, true)
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(120, 24)
	m.modelsRoot = t.TempDir()
	m.helperInstalledFn = func() bool { return true }
	m.refreshInstallState()

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	m.cursor = findVoiceRow(m, vrowCustomPath)
	if m.cursor < 0 {
		t.Fatal("custom path row missing")
	}

	m.Update(enter)
	m.input.SetValue(t.TempDir())
	_, cmd := m.Update(enter)
	if cmd != nil {
		t.Fatal("invalid path produced a command")
	}
	if m.customErr == "" {
		t.Fatal("no error for an invalid path")
	}
	if m.cfg.CustomModelDir != "" {
		t.Fatal("invalid path stored")
	}
	if !strings.Contains(m.View().Content, "invalid:") {
		t.Fatalf("error not rendered:\n%s", m.View().Content)
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "model.int8.onnx"), []byte("x"), 0o644)
	m.cfg.Verified = true
	m.cursor = findVoiceRow(m, vrowCustomPath)
	m.Update(enter)
	m.input.SetValue(dir)
	_, cmd = m.Update(enter)
	if cmd != nil {
		t.Fatal("valid path produced a command")
	}
	if m.cfg.CustomModelDir != dir || m.cfg.Verified || !m.modified {
		t.Fatalf("staged custom path = %q verified=%v modified=%v", m.cfg.CustomModelDir, m.cfg.Verified, m.modified)
	}
	if got := loadVoiceSettings(database, mk); got.CustomModelDir != "" {
		t.Fatal("custom path persisted before save")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.CustomModelDir != dir || !chg.keepEngine {
		t.Fatalf("save msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); got.CustomModelDir != dir {
		t.Fatalf("persisted custom dir = %q", got.CustomModelDir)
	}
	m.saveDone(chg.cfg)

	if !voiceSetupReady(m.cfg, m.modelsRoot) {
		t.Fatal("custom path not counted as model present")
	}
	gotDir, gotKind := localModelTarget(m.cfg, m.modelsRoot)
	if gotDir != dir || gotKind != voice.ModelKindSenseVoice {
		t.Fatalf("set_model target = %q %q", gotDir, gotKind)
	}
	if want := truncateVoiceValue("[active] "+dir, m.valueWidth()); !strings.Contains(m.View().Content, want) {
		t.Fatalf("custom path not marked active: want %q", want)
	}

	m.cursor = findVoiceRow(m, vrowCustomPath)
	m.Update(enter)
	m.input.SetValue("")
	m.Update(enter)
	if m.cfg.CustomModelDir != "" {
		t.Fatal("clear not staged")
	}
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok = cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.CustomModelDir != "" {
		t.Fatalf("clear save msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); got.CustomModelDir != "" {
		t.Fatal("clear not persisted")
	}
}

func TestVoiceSettingsPrecisionToggle(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)
	m.cfg.Verified = true

	m.cursor = findVoiceRow(m, vrowPrecision)
	if m.cursor < 0 || m.rows()[m.cursor].kind != vrowPrecision {
		t.Fatalf("precision row missing: %+v", m.rows())
	}
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if cmd != nil {
		t.Fatal("precision toggle must be staged")
	}
	if !m.cfg.ModelInt8 || m.cfg.Verified || !m.modified {
		t.Fatalf("staged precision: int8=%v verified=%v modified=%v", m.cfg.ModelInt8, m.cfg.Verified, m.modified)
	}
	if got := loadVoiceSettings(database, mk); got.ModelInt8 {
		t.Fatal("int8 persisted before save")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || !chg.cfg.ModelInt8 || !chg.keepEngine {
		t.Fatalf("save msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); !got.ModelInt8 {
		t.Fatal("int8 not persisted")
	}
	if _, kind := localModelTarget(m.cfg, m.modelsRoot); kind != voice.ModelKindSenseVoiceInt8 {
		t.Fatalf("kind = %q", kind)
	}
	if !strings.Contains(m.View().Content, "int8") {
		t.Fatal("precision not rendered")
	}

	m.cfg.ModelID = voice.ModelCatalog()[1].ID
	if findVoiceRow(m, vrowPrecision) >= 0 {
		t.Fatal("precision row shown for paraformer")
	}
	if _, kind := localModelTarget(m.cfg, m.modelsRoot); kind != voice.ModelKindParaformer {
		t.Fatalf("paraformer kind = %q", kind)
	}
}

func TestVoiceSettingsPrecisionCustomDir(t *testing.T) {
	m := newVoiceSettingsModel(nil, nil, defaultVoiceSettings())
	m.cfg.ModelInt8 = true

	both := t.TempDir()
	os.WriteFile(filepath.Join(both, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(both, "model.onnx"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(both, "model.int8.onnx"), []byte("x"), 0o644)
	m.cfg.CustomModelDir = both
	if findVoiceRow(m, vrowPrecision) < 0 {
		t.Fatal("precision row missing for a two-weights custom dir")
	}
	if _, kind := localModelTarget(m.cfg, m.modelsRoot); kind != voice.ModelKindSenseVoiceInt8 {
		t.Fatalf("custom dir kind = %q", kind)
	}

	fp32only := t.TempDir()
	os.WriteFile(filepath.Join(fp32only, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(fp32only, "model.onnx"), []byte("x"), 0o644)
	m.cfg.CustomModelDir = fp32only
	if findVoiceRow(m, vrowPrecision) >= 0 {
		t.Fatal("precision row shown for a single-weight custom dir")
	}
	if _, kind := localModelTarget(m.cfg, m.modelsRoot); kind != voice.ModelKindSenseVoice {
		t.Fatalf("single-weight custom dir kind = %q", kind)
	}
}

func TestVoiceSettingsEnginePickerOrder(t *testing.T) {
	descs := enginePickerDescriptors()
	if len(descs) < 2 || descs[0].ID != voiceEngineLocal || descs[1].ID != voiceEngineVolcano {
		t.Fatalf("picker order = %v", descs)
	}
	seen := map[string]bool{}
	for _, d := range descs {
		if seen[d.ID] {
			t.Fatalf("duplicate %q in picker", d.ID)
		}
		seen[d.ID] = true
	}
	if len(seen) != len(voice.EngineDescriptors()) {
		t.Fatal("picker lost an engine")
	}
}

func TestVoiceSettingsLegacyModelIDMigration(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(database, voiceModelSettingKey, "sensevoice-int8"); err != nil {
		t.Fatal(err)
	}
	cfg := loadVoiceSettings(database, nil)
	if cfg.ModelID != voice.ModelCatalog()[0].ID || !cfg.ModelInt8 {
		t.Fatalf("migrated = %q int8=%v", cfg.ModelID, cfg.ModelInt8)
	}
	if err := db.SetSetting(database, voiceModelSettingKey, "sensevoice-fp32"); err != nil {
		t.Fatal(err)
	}
	cfg = loadVoiceSettings(database, nil)
	if cfg.ModelID != voice.ModelCatalog()[0].ID || cfg.ModelInt8 {
		t.Fatalf("migrated = %q int8=%v", cfg.ModelID, cfg.ModelInt8)
	}
}

func TestVoiceSettingsEngineSwitchStagedUntilSave(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	for i, d := range enginePickerDescriptors() {
		if d.ID == voiceEngineVolcano {
			m.cursor = i
		}
	}
	m.Update(enter)
	if m.cfg.Engine != voiceEngineVolcano || !m.modified {
		t.Fatal("engine switch not staged")
	}
	if got := loadVoiceSettings(database, mk); got.Engine != voiceEngineLocal {
		t.Fatal("engine persisted before save")
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if m.cfg.Engine != voiceEngineLocal || m.modified {
		t.Fatalf("reset did not restore the engine: %q modified=%v", m.cfg.Engine, m.modified)
	}
}

var errTest = errors.New("boom")

func TestVoiceSettingsHelperUpdate(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)
	m.helperInstalledFn = func() bool { return true }
	ver := "v3.0.0"
	m.helperVersionFn = func() string { return ver }
	m.refreshInstallState()

	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	if view := m.View().Content; !strings.Contains(view, "installed (v3.0.0)") {
		t.Fatalf("version not rendered:\n%s", view)
	}

	m.cursor = findVoiceRow(m, vrowHelper)
	_, cmd := m.Update(enter)
	if _, ok := cmd().(voiceHelperUpdateCheckRequestMsg); !ok {
		t.Fatalf("update check request = %#v", cmd())
	}
	if !m.checkingUpdate {
		t.Fatal("check not marked in flight")
	}

	m.updateCheckDone("v3.1.0", nil)
	if view := m.View().Content; !strings.Contains(view, "update v3.0.0 -> v3.1.0") {
		t.Fatalf("update not offered:\n%s", view)
	}
	_, cmd = m.Update(enter)
	req, ok := cmd().(voiceDownloadRequestMsg)
	if !ok || req.target != voiceHelperTarget {
		t.Fatalf("update download request = %#v", cmd())
	}

	m.downloadStarted(voiceHelperTarget)
	ver = "v3.1.0"
	m.downloadUpdate(voiceDownloadMsg{target: voiceHelperTarget, done: true})
	if view := m.View().Content; !strings.Contains(view, "installed (v3.1.0)") {
		t.Fatalf("new version not rendered:\n%s", view)
	}
	if m.updateTag != "" {
		t.Fatal("stale update tag after reinstall")
	}

	_, cmd = m.Update(enter)
	cmd()
	m.updateCheckDone("v3.1.0", nil)
	if view := m.View().Content; !strings.Contains(view, "up to date") {
		t.Fatalf("up-to-date not rendered:\n%s", view)
	}
	m.updateCheckDone("", errTest)
	if view := m.View().Content; !strings.Contains(view, "update check failed: boom") {
		t.Fatalf("check failure not rendered:\n%s", view)
	}

	ver = "dev"
	m.refreshInstallState()
	if view := m.View().Content; !strings.Contains(view, "installed (unknown version)") {
		t.Fatalf("unknown version not rendered:\n%s", view)
	}
}

func TestVoiceHelperUpdateCheckFlow(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	old := latestHelperVersionFn
	latestHelperVersionFn = func(context.Context) (string, error) { return "v9.9.9", nil }
	t.Cleanup(func() { latestHelperVersionFn = old })

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceHelperUpdateCheckRequestMsg{})
	a = upd.(App)
	msg, ok := cmd().(voiceHelperUpdateCheckMsg)
	if !ok || msg.tag != "v9.9.9" || msg.err != nil {
		t.Fatalf("check msg = %#v", msg)
	}
	upd, _ = a.Update(msg)
	a = upd.(App)
	if a.voiceTab().updateTag != "v9.9.9" {
		t.Fatal("tab not updated with the latest tag")
	}
}

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
	a.width = 80
	a.height = 40
	var got string
	a.voiceDownload = func(target string, progress func(float64)) error {
		got = target
		progress(50)
		return nil
	}

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	a.voiceTab().helperInstalledFn = func() bool { return true }

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
	if a.voiceTab().dlTarget != "" {
		t.Fatal("tab download state not cleared")
	}
	if !a.voiceTab().helperOK {
		t.Fatal("helper state not refreshed")
	}

	a.voiceDownload = func(string, func(float64)) error { return errTest }
	upd, cmd = a.Update(voiceDownloadRequestMsg{target: voice.ModelCatalog()[0].ID})
	a = upd.(App)
	a = drainVoiceDownload(t, a, cmd)
	if !strings.Contains(a.voiceTab().View().Content, "failed: boom") {
		t.Fatal("failed download not shown in tab")
	}
}

func TestVoiceTestRecordingFlow(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.width = 80
	a.height = 40
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
	if !a.voiceTab().testing {
		t.Fatal("tab not in testing state")
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

	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventInfo, Msg: "downloading silero_vad.onnx"}})
	a = upd.(App)
	if !a.voiceTest {
		t.Fatal("info event aborted the test")
	}

	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventPartial, Text: "ni hao"}})
	a = upd.(App)
	if a.voiceTab().testText != "ni hao" {
		t.Fatalf("partial = %q", a.voiceTab().testText)
	}

	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = append(a.tabs, Tab{Type: SSHTab, Title: "prod", Model: sv})

	upd, cmd = a.Update(voiceFinalMsg("hello test"))
	a = upd.(App)
	if a.voiceTest {
		t.Fatal("test still active after final")
	}
	if !a.voiceCfg.Verified {
		t.Fatal("verified not set")
	}
	if !strings.Contains(a.voiceTab().View().Content, "hello test") {
		t.Fatal("transcript not shown in tab")
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

	upd, _ = a.Update(voiceTestTimeoutMsg{seq: a.voiceTestSeq})
	a = upd.(App)
	if a.voiceBusy {
		t.Fatal("stale timeout restarted a stop")
	}
}

func TestVoiceTestBlockedWhenNotReady(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.width = 80
	a.height = 40
	a.voiceReady = func(voiceSettings) bool { return false }

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	upd, cmd := a.Update(voiceTestRequestMsg{})
	a = upd.(App)
	if a.voiceTest || cmd != nil {
		t.Fatal("test started without a complete setup")
	}
	if !strings.Contains(a.voiceTab().View().Content, "setup incomplete") {
		t.Fatal("no guidance shown")
	}
	if fe.started {
		t.Fatal("engine started")
	}
}

func TestVoiceSettingsModelChangeAppliesToEngine(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceEngine = fe

	cfg := defaultVoiceSettings()
	cfg.ModelID = voice.ModelCatalog()[1].ID
	upd, _ := a.Update(voiceSettingsChangedMsg{cfg: cfg, keepEngine: true})
	a = upd.(App)
	if fe.modelKind != voice.ModelKindParaformer {
		t.Fatalf("kind = %q", fe.modelKind)
	}
	if !strings.HasSuffix(fe.modelDir, voice.ModelCatalog()[1].Dir) {
		t.Fatalf("dir = %q", fe.modelDir)
	}
}

func TestVoiceSettingsCloseStopsTest(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
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

	upd, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	a = upd.(App)
	if cmd == nil {
		t.Fatal("esc produced no close command")
	}
	upd, cmd = a.Update(cmd())
	a = upd.(App)
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStoppedMsg)
		return ok
	}) {
		upd, _ = a.Update(m)
		a = upd.(App)
	}
	if a.voiceTab() != nil {
		t.Fatal("esc did not close the voice tab")
	}
	if a.voiceTest {
		t.Fatal("test still active after close")
	}
	if !fe.stopped {
		t.Fatal("engine not stopped after close")
	}
}

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

	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}

	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventState, State: voice.StateIdle}})
	a = upd.(App)
	if a.voiceSwallowFinal {
		t.Fatal("state idle did not clear the swallow")
	}

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

func TestVoiceTestRejectedWhileDictating(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.width = 80
	a.height = 40
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
	if !strings.Contains(a.voiceTab().View().Content, "stop dictation") {
		t.Fatalf("no refusal shown:\n%s", a.voiceTab().View().Content)
	}

	upd, _ = a.Update(voiceEventMsg{ev: voice.Event{Type: voice.EventPartial, Text: "dictating"}})
	a = upd.(App)
	if a.voiceTab().testText != "" {
		t.Fatal("partial leaked into the tab")
	}
}

func TestVoiceContextSettingTogglePersists(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 24)
	if got := loadVoiceSettings(database, mk); got.Context {
		t.Fatal("context default on")
	}

	m.cfg.Engine = voiceEngineVolcano
	m.saved.Engine = voiceEngineVolcano
	m.cursor = findVoiceRow(m, vrowContext)
	if m.cursor < 0 {
		t.Fatal("context row missing")
	}
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if cmd != nil {
		t.Fatal("context toggle must be staged")
	}
	if !m.cfg.Context || !m.modified {
		t.Fatal("context toggle not staged")
	}
	if got := loadVoiceSettings(database, mk); got.Context {
		t.Fatal("context persisted before save")
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || !chg.cfg.Context || !chg.keepEngine {
		t.Fatalf("save msg = %#v", chg)
	}
	if got := loadVoiceSettings(database, mk); !got.Context {
		t.Fatal("context toggle not persisted")
	}
	if view := m.View().Content; !strings.Contains(view, "Context awareness") || !strings.Contains(view, "on") {
		t.Fatalf("context row not rendered:\n%s", view)
	}
}

func TestVoiceContextAIHistoryTurns(t *testing.T) {
	var msgs []map[string]string
	msgs = append(msgs, map[string]string{"role": "system", "content": "be helpful"})
	for i := 0; i < 12; i++ {
		msgs = append(msgs, map[string]string{"role": "user", "content": fmt.Sprintf("question %d", i)})
		msgs = append(msgs, map[string]string{"role": "assistant", "content": fmt.Sprintf("answer %d", i)})
	}
	msgs = append(msgs, map[string]string{"role": "tool", "content": "tool output"})
	history, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	b := &aiBridge{agent: &historyAgent{history: history}}

	turns := b.voiceContextTurns(voice.DefaultContextMaxTurns)
	if len(turns) != 20 {
		t.Fatalf("turns = %d, want 20", len(turns))
	}
	if turns[0].Speaker != "user" || turns[0].Text != "question 2" {
		t.Fatalf("oldest turn = %+v", turns[0])
	}
	if turns[1].Speaker != "bot" || turns[1].Text != "answer 2" {
		t.Fatalf("second turn = %+v", turns[1])
	}
	if turns[19].Text != "answer 11" {
		t.Fatalf("newest turn = %+v", turns[19])
	}
}

func TestVoiceContextStringFromAIOverlay(t *testing.T) {
	history, err := json.Marshal([]map[string]string{
		{"role": "user", "content": "how do I list pods"},
		{"role": "assistant", "content": "kubectl get pods"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.aiBridge = &aiBridge{agent: &historyAgent{history: history}}
	fake := aiview.NewFakeRunner()
	a.aiView = aiview.New(fake, fake, fake)
	a.aiVisible = true
	a.voiceCfg.Context = true

	got := a.voiceContextString()
	if !strings.Contains(got, `"context_type":"dialog_ctx"`) {
		t.Fatalf("context = %q", got)
	}
	if !strings.Contains(got, "kubectl get pods") {
		t.Fatalf("assistant turn missing: %q", got)
	}
	if strings.Contains(got, "corpus\x00") {
		t.Fatal("junk in context")
	}

	a.voiceCfg.Context = false
	if got := a.voiceContextString(); got != "" {
		t.Fatalf("context not cleared: %q", got)
	}
}

func TestVoiceContextStringFromTerminalTab(t *testing.T) {
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	feedSSHChunk(sv, "┌──────────┐\r\n│ degraded │\r\n└──────────┘\r\nkubectl   get   pods\r\nkubectl get pods\r\n$ ")

	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	a.activeTab = 0
	a.voiceCfg.Context = true

	got := a.voiceContextString()
	if !strings.Contains(got, "kubectl get pods") {
		t.Fatalf("terminal context = %q", got)
	}
	if !strings.Contains(got, "degraded") {
		t.Fatalf("bordered text lost: %q", got)
	}
	if strings.ContainsAny(got, "│┌┐└┘─") {
		t.Fatalf("border runes leaked: %q", got)
	}
}

func TestVoiceToggleSetsEngineContextProvider(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.voiceCfg.Context = true
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	feedSSHChunk(sv, "kubectl get pods\r\n")
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	a.activeTab = 0

	upd, cmd := a.toggleVoice()
	a = upd
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}
	if fe.contextFn == nil {
		t.Fatal("engine got no context provider")
	}
	if got := fe.contextFn(); !strings.Contains(got, `"dialog_ctx"`) || !strings.Contains(got, "kubectl get pods") {
		t.Fatalf("provider context = %q", got)
	}

	// The provider reads live state: newly fed terminal content shows up
	// without restarting the recording.
	feedSSHChunk(sv, "docker compose up\r\n")
	if got := fe.contextFn(); !strings.Contains(got, "docker compose up") {
		t.Fatalf("provider did not pick up fresh transcript: %q", got)
	}

	upd, cmd = a.toggleVoice()
	a = upd
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStoppedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}

	a.voiceCfg.Context = false
	upd, cmd = a.toggleVoice()
	a = upd
	for _, m := range collectCmdMsgs(t, cmd, func(m tea.Msg) bool {
		_, ok := m.(voiceStartedMsg)
		return ok
	}) {
		upd2, _ := a.Update(m)
		a = upd2.(App)
	}
	if got := fe.contextFn(); got != "" {
		t.Fatalf("provider not cleared with the switch off: %q", got)
	}
}

func TestVoiceContextTerminalTailUsesNewestContent(t *testing.T) {
	sink := &syncWriteCloser{}
	is := &internalssh.InteractiveSession{Stdin: sink, Done: make(chan error, 1)}
	sv := sshview.New(is, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))

	var b strings.Builder
	b.WriteString("OLDMARKER earliest scrollback content\r\n")
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&b, "filler line %04d xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\r\n", i)
	}
	b.WriteString("NEWMARKER kubectl get pods\r\n")
	feedSSHChunk(sv, b.String())

	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.tabs = []Tab{{Type: SSHTab, Title: "prod", Model: sv}}
	a.activeTab = 0
	a.voiceCfg.Context = true

	got := a.voiceContextString()
	if !strings.Contains(got, "NEWMARKER") {
		t.Fatalf("newest content missing from context: %q", got)
	}
	if strings.Contains(got, "OLDMARKER") {
		t.Fatalf("oldest scrollback leaked into context: %q", got)
	}
}

func TestVoiceSettingsExtraRows(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.UnlockNoPassword()
	m := newVoiceSettingsModel(database, mk, defaultVoiceSettings())
	m.SetSize(80, 30)

	if findVoiceRow(m, vrowContext) >= 0 || findVoiceRow(m, vrowDDC) >= 0 {
		t.Fatal("local engine shows the volcano feature rows")
	}
	m.cfg.Engine = voiceEngineVolcano
	if findVoiceRow(m, vrowContext) < 0 || findVoiceRow(m, vrowDDC) < 0 {
		t.Fatal("volcano missing the extra feature rows")
	}
	if got := loadVoiceSettings(database, mk); !got.DDC {
		t.Fatal("DDC default off")
	}

	m.cursor = findVoiceRow(m, vrowDDC)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.cfg.DDC || !m.modified {
		t.Fatal("DDC toggle not staged")
	}
	if got := loadVoiceSettings(database, mk); !got.DDC {
		t.Fatal("DDC persisted before save")
	}

	m.cursor = findVoiceRow(m, vrowContext)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if !m.cfg.Context {
		t.Fatal("context toggle not staged")
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	chg, ok := cmd().(voiceSettingsChangedMsg)
	if !ok || chg.cfg.DDC || !chg.cfg.Context || chg.keepEngine {
		t.Fatalf("save msg = %#v", chg)
	}
	m.saveDone(chg.cfg)
	got := loadVoiceSettings(database, mk)
	if got.DDC || !got.Context {
		t.Fatalf("persisted = %+v", got)
	}
	if view := m.View().Content; !strings.Contains(view, "Semantic smoothing (DDC)") || !strings.Contains(view, "Volcano features") {
		t.Fatalf("volcano feature rows not rendered:\n%s", view)
	}
}

func TestVoiceSettingsTabScrolls(t *testing.T) {
	m := newVoiceSettingsModel(nil, nil, defaultVoiceSettings())
	m.SetSize(80, 10)
	total := len(m.buildScrollLines())
	if total <= m.visibleRows() {
		t.Fatalf("test needs more lines than the %d visible", m.visibleRows())
	}

	last := len(m.rows()) - 1
	for i := 0; i < last; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	}
	if m.cursor != last {
		t.Fatalf("cursor = %d, want %d", m.cursor, last)
	}
	if lines := strings.Split(m.View().Content, "\n"); len(lines) > m.height {
		t.Fatalf("view lines = %d, want <= %d", len(lines), m.height)
	}
	if m.scroll == 0 {
		t.Fatal("cursor past the viewport did not scroll")
	}

	scrolled := m.scroll
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.scroll != scrolled-1 {
		t.Fatalf("wheel up scroll = %d, want %d", m.scroll, scrolled-1)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if m.scroll != scrolled {
		t.Fatalf("wheel down scroll = %d, want %d", m.scroll, scrolled)
	}

	for i := 0; i < last; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	}
	m.View()
	if m.scroll > 1 {
		t.Fatalf("scroll did not follow the cursor back up: %d", m.scroll)
	}
}

func TestVoiceHotkeySkippedInVoiceTab(t *testing.T) {
	fe := &fakeVoiceEngine{events: make(chan voice.Event)}
	a := voiceTestApp(fe)
	a.masterKey = security.NewMasterKeyManager(nil, nil, time.Minute)
	a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: nil}}

	upd, _ := a.Update(openVoiceSettingsMsg{})
	a = upd.(App)
	vt := a.voiceTab()
	if vt == nil {
		t.Fatal("voice tab did not open")
	}

	vt.cursor = findVoiceRow(vt, vrowThreshold)
	upd, _ = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	a = upd.(App)
	if !vt.modified {
		t.Fatal("threshold change not staged")
	}

	upd, _ = a.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	a = upd.(App)
	if a.voiceRec {
		t.Fatal("voice hotkey fired inside the voice tab")
	}
	if a.voiceEngine != nil {
		t.Fatal("engine was built")
	}
	if vt.modified {
		t.Fatal("ctrl+r did not reset the staged changes")
	}
}

func TestVoiceSettingsDDCPersistenceRoundTrip(t *testing.T) {
	database, err := db.InitDB(t.TempDir() + "/voice.db")
	if err != nil {
		t.Fatal(err)
	}
	if got := loadVoiceSettings(database, nil); !got.DDC {
		t.Fatal("DDC default off on an empty database")
	}
	if err := db.SetSetting(database, voiceDDCSettingKey, "0"); err != nil {
		t.Fatal(err)
	}
	if got := loadVoiceSettings(database, nil); got.DDC {
		t.Fatal("stored DDC=0 not honored")
	}
	cfg := defaultVoiceSettings()
	cfg.DDC = false
	if err := persistVoiceSettings(database, nil, cfg); err != nil {
		t.Fatal(err)
	}
	if got := loadVoiceSettings(database, nil); got.DDC {
		t.Fatal("DDC=0 round trip failed")
	}
}
