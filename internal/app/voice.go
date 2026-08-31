package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/voice"
)

const (
	voiceEngineLocal   = "local"
	voiceEngineVolcano = "volcano"

	voiceEngineSettingKey      = "voice_engine"
	voiceVADSettingKey         = "voice_vad_threshold"
	voiceSilenceSettingKey     = "voice_vad_silence_ms"
	voiceSentenceEndSettingKey = "voice_sentence_end"
	voiceModelSettingKey       = "voice_model"
	voiceCustomModelSettingKey = "voice_custom_model" // absolute dir, local engine
	voiceVerifiedSettingKey    = "voice_verified"
	voiceParamsSettingPrefix   = "voice_params_" // + engine id: encrypted JSON params blob
	voiceVolcanoSettingKey     = "voice_volcano" // legacy: migrated into voice_params_volcano
)

// voiceSettings holds the voice input configuration. VADThreshold 0 keeps the
// engine default. Params carries the per-engine values described by each
// engine descriptor (defaults applied on load).
type voiceSettings struct {
	Engine         string
	VADThreshold   float64
	VADSilenceMs   int // trailing silence that ends an utterance, milliseconds
	SentenceEnd    voice.SentenceEnd
	Params         map[string]map[string]string // engine id -> param key -> value
	ModelID        string                       // offline model catalog id (local engine)
	CustomModelDir string                       // validated custom model dir; overrides ModelID when set
	Verified       bool                         // a test recording succeeded with this setup
}

// defaultEngineParams seeds every registered engine with its ParamSpec
// defaults so a missing stored blob falls back cleanly.
func defaultEngineParams() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, d := range voice.EngineDescriptors() {
		params := map[string]string{}
		for _, p := range d.Params {
			if p.Default != "" {
				params[p.Key] = p.Default
			}
		}
		out[d.ID] = params
	}
	return out
}

func defaultVoiceSettings() voiceSettings {
	return voiceSettings{
		Engine:       voiceEngineLocal,
		VADSilenceMs: 1000,
		SentenceEnd:  voice.SentenceEndSpace,
		ModelID:      voice.ModelCatalog()[0].ID,
		Params:       defaultEngineParams(),
	}
}

// engineParams returns the configured values for one engine (descriptor
// defaults when nothing was loaded).
func (cfg voiceSettings) engineParams(id string) map[string]string {
	if p := cfg.Params[id]; p != nil {
		return p
	}
	params := map[string]string{}
	if d, ok := voice.EngineDescriptorByID(id); ok {
		for _, spec := range d.Params {
			if spec.Default != "" {
				params[spec.Key] = spec.Default
			}
		}
	}
	return params
}

func (cfg *voiceSettings) setEngineParam(id, key, value string) {
	if cfg.Params == nil {
		cfg.Params = map[string]map[string]string{}
	}
	p := cfg.Params[id]
	if p == nil {
		p = map[string]string{}
		cfg.Params[id] = p
	}
	p[key] = value
}

func (cfg voiceSettings) vadParams() voice.VADParams {
	return voice.VADParams{
		Threshold:       cfg.VADThreshold,
		TrailingSilence: float64(cfg.VADSilenceMs) / 1000,
	}
}

func loadVoiceSettings(database *gorm.DB, mk *security.MasterKeyManager) voiceSettings {
	cfg := defaultVoiceSettings()
	if database == nil {
		return cfg
	}
	migrateLegacyVolcanoParams(database, mk)
	if v, err := db.GetSetting(database, voiceEngineSettingKey); err == nil && v != "" {
		// unregistered ids are kept so the panel can render them generically
		cfg.Engine = v
	}
	if v, err := db.GetSetting(database, voiceVADSettingKey); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.VADThreshold = f
		}
	}
	if v, err := db.GetSetting(database, voiceSilenceSettingKey); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 50 && n <= 5000 {
			cfg.VADSilenceMs = n
		}
	}
	if v, err := db.GetSetting(database, voiceSentenceEndSettingKey); err == nil {
		switch voice.SentenceEnd(v) {
		case voice.SentenceEndEnter, voice.SentenceEndSpace:
			cfg.SentenceEnd = voice.SentenceEnd(v)
		}
	}
	if v, err := db.GetSetting(database, voiceModelSettingKey); err == nil && v != "" {
		if m := voice.ModelByID(v); m.ID == v {
			cfg.ModelID = v
		}
	}
	if v, err := db.GetSetting(database, voiceCustomModelSettingKey); err == nil {
		cfg.CustomModelDir = v
	}
	if v, err := db.GetSetting(database, voiceVerifiedSettingKey); err == nil {
		cfg.Verified = v == "1"
	}
	for id := range cfg.Params {
		if stored := loadEngineParams(database, mk, id); stored != nil {
			for k, v := range stored {
				cfg.Params[id][k] = v
			}
		}
	}
	return cfg
}

// loadEngineParams reads and decrypts the voice_params_<id> blob; nil when
// absent or unreadable (defaults then apply).
func loadEngineParams(database *gorm.DB, mk *security.MasterKeyManager, id string) map[string]string {
	enc, err := db.GetSetting(database, voiceParamsSettingPrefix+id)
	if err != nil || enc == "" || mk == nil {
		return nil
	}
	k := mk.GetKey()
	if k == nil {
		return nil
	}
	plain, err := security.Decrypt(enc, k.Bytes())
	k.Clear()
	if err != nil {
		return nil
	}
	var params map[string]string
	if json.Unmarshal(plain, &params) != nil {
		return nil
	}
	return params
}

// migrateLegacyVolcanoParams moves the old voice_volcano key blob into the
// voice_params_volcano schema, then deletes the old key. A no-op when the
// new blob already exists or the master key is unavailable.
func migrateLegacyVolcanoParams(database *gorm.DB, mk *security.MasterKeyManager) {
	if _, err := db.GetSetting(database, voiceParamsSettingPrefix+voiceEngineVolcano); err == nil {
		return
	}
	enc, err := db.GetSetting(database, voiceVolcanoSettingKey)
	if err != nil || enc == "" || mk == nil {
		return
	}
	k := mk.GetKey()
	if k == nil {
		return
	}
	plain, err := security.Decrypt(enc, k.Bytes())
	k.Clear()
	if err != nil {
		return
	}
	var keys struct {
		APIKey    string `json:"api_key"`
		AppKey    string `json:"app_key"`
		AccessKey string `json:"access_key"`
	}
	if json.Unmarshal(plain, &keys) != nil {
		return
	}
	params := map[string]string{
		"api_key":    keys.APIKey,
		"app_key":    keys.AppKey,
		"access_key": keys.AccessKey,
	}
	if persistEngineParams(database, mk, voiceEngineVolcano, params) != nil {
		return
	}
	database.Unscoped().Where("key = ?", voiceVolcanoSettingKey).Delete(&db.AppSetting{})
}

func persistVoiceSettings(database *gorm.DB, mk *security.MasterKeyManager, cfg voiceSettings) error {
	if err := db.SetSetting(database, voiceEngineSettingKey, cfg.Engine); err != nil {
		return err
	}
	if err := db.SetSetting(database, voiceVADSettingKey, strconv.FormatFloat(cfg.VADThreshold, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.SetSetting(database, voiceSilenceSettingKey, strconv.Itoa(cfg.VADSilenceMs)); err != nil {
		return err
	}
	if err := db.SetSetting(database, voiceSentenceEndSettingKey, string(cfg.SentenceEnd)); err != nil {
		return err
	}
	if err := db.SetSetting(database, voiceModelSettingKey, cfg.ModelID); err != nil {
		return err
	}
	if err := db.SetSetting(database, voiceCustomModelSettingKey, cfg.CustomModelDir); err != nil {
		return err
	}
	verified := "0"
	if cfg.Verified {
		verified = "1"
	}
	if err := db.SetSetting(database, voiceVerifiedSettingKey, verified); err != nil {
		return err
	}
	for id, params := range cfg.Params {
		if len(params) == 0 {
			if d, ok := voice.EngineDescriptorByID(id); ok && len(d.Params) == 0 {
				continue // param-less engine: no blob to store
			}
		}
		if err := persistEngineParams(database, mk, id, params); err != nil {
			return err
		}
	}
	return nil
}

// persistEngineParams encrypts and stores one engine's params blob. Without
// an unlocked master key the stored blob is left untouched.
func persistEngineParams(database *gorm.DB, mk *security.MasterKeyManager, id string, params map[string]string) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if mk == nil {
		return nil
	}
	k := mk.GetKey()
	if k == nil {
		return nil
	}
	defer k.Clear()
	enc, err := security.Encrypt(data, k.Bytes())
	if err != nil {
		return err
	}
	return db.SetSetting(database, voiceParamsSettingPrefix+id, enc)
}

// defaultVoiceEngine builds the configured engine from its registered
// descriptor; onProgress reports the helper binary download (the model
// download arrives as engine events).
func defaultVoiceEngine(cfg voiceSettings, onProgress func(float64)) (voice.Engine, error) {
	d, ok := voice.EngineDescriptorByID(cfg.Engine)
	if !ok {
		return nil, fmt.Errorf("voice: unknown engine %q", cfg.Engine)
	}
	return d.New(cfg.engineParams(cfg.Engine), voice.FeedDeps{
		VAD:                cfg.vadParams(),
		OnDownloadProgress: onProgress,
	})
}

// helperInstalledFn reports whether the helper binary is installed; a
// package var so tests can fake it (the dev machine may have a real helper).
var helperInstalledFn = voice.HelperInstalled

// localModelTarget returns the model dir/kind for set_model: the custom
// directory when configured, else the selected catalog model.
func localModelTarget(cfg voiceSettings, modelsRoot string) (dir, kind string) {
	if cfg.CustomModelDir != "" {
		return cfg.CustomModelDir, voice.ModelKindSenseVoice
	}
	spec := voice.ModelByID(cfg.ModelID)
	return spec.ModelDir(modelsRoot), spec.Kind
}

// localModelReady reports whether the local engine has a usable model: a
// valid custom directory counts as present.
func localModelReady(cfg voiceSettings, modelsRoot string) bool {
	if cfg.CustomModelDir != "" {
		return voice.ValidCustomModelDir(cfg.CustomModelDir)
	}
	return voice.ModelByID(cfg.ModelID).Installed(modelsRoot)
}

// voiceSetupReady reports whether ctrl+r can record directly: the engine
// descriptor's params must be ready; local additionally needs the helper
// binary and a model on disk.
func voiceSetupReady(cfg voiceSettings, modelsRoot string) bool {
	return voiceSetupIssue(cfg, modelsRoot) == ""
}

// voiceSetupIssue describes the first unmet setup step, "" when ready.
func voiceSetupIssue(cfg voiceSettings, modelsRoot string) string {
	d, ok := voice.EngineDescriptorByID(cfg.Engine)
	if !ok {
		return "unknown engine " + cfg.Engine
	}
	if !d.Ready(cfg.engineParams(cfg.Engine)) {
		if missing := voice.FirstMissingParam(d, cfg.engineParams(cfg.Engine)); missing != "" {
			return "enter the " + missing + " first"
		}
		return "complete the " + d.Label + " settings first"
	}
	if cfg.Engine == voiceEngineLocal {
		if !helperInstalledFn() {
			return "download the helper binary first"
		}
		if !localModelReady(cfg, modelsRoot) {
			return "download a model first"
		}
	}
	return ""
}

// voiceReadyFn returns the readiness check (a.voiceReady overrides in tests).
func (a App) voiceReadyFn() func(voiceSettings) bool {
	if a.voiceReady != nil {
		return a.voiceReady
	}
	return func(cfg voiceSettings) bool { return voiceSetupReady(cfg, voice.ModelsRoot()) }
}

// defaultVoiceDownload fetches the helper binary or a catalog model,
// reporting progress. target is "helper" or a model ID.
func defaultVoiceDownload(target string, onProgress func(float64)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if target == voiceHelperTarget {
		return voice.DownloadHelper(ctx, "", onProgress)
	}
	return voice.DownloadModel(ctx, voice.ModelByID(target), voice.ModelsRoot(), "", onProgress)
}

func waitVoiceDownload(ch <-chan voiceDownloadMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return voiceEngineClosedMsg{}
		}
		return msg
	}
}

func voiceTestTick(seq int) tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return voiceTestTimeoutMsg{seq: seq} })
}

func (a App) ensureVoiceCfg() App {
	if a.voiceCfgLoaded {
		return a
	}
	a.voiceCfg = loadVoiceSettings(a.db, a.masterKey)
	a.voiceCfgLoaded = true
	return a
}

// ensureVoice builds the voice engine once (it persists across toggles and
// page switches) and arms the event/progress pumps.
func (a App) ensureVoice() (App, tea.Cmd) {
	if a.voiceEngine != nil {
		return a, nil
	}
	a = a.ensureVoiceCfg()
	if a.voiceProgressCh == nil {
		a.voiceProgressCh = make(chan float64, 64)
	}
	factory := a.voiceMake
	if factory == nil {
		factory = defaultVoiceEngine
	}
	progressCh := a.voiceProgressCh
	eng, err := factory(a.voiceCfg, func(pct float64) {
		select {
		case progressCh <- pct:
		default:
		}
	})
	if err != nil {
		return a, func() tea.Msg { return types.ErrorMsg{Err: err} }
	}
	a.voiceEngine = eng
	a.voiceName = a.voiceCfg.Engine
	if a.voiceCfg.Engine == voiceEngineLocal {
		dir, kind := localModelTarget(a.voiceCfg, voice.ModelsRoot())
		_ = eng.SetModel(dir, kind)
	}
	cmds := []tea.Cmd{waitVoiceEvent(eng.Events())}
	if !a.voiceProgressArmed {
		a.voiceProgressArmed = true
		cmds = append(cmds, waitVoiceProgress(progressCh))
	}
	return a, tea.Batch(cmds...)
}

func waitVoiceEvent(ch <-chan voice.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return voiceEngineClosedMsg{}
		}
		return voiceEventMsg{ev: ev}
	}
}

func waitVoiceProgress(ch <-chan float64) tea.Cmd {
	return func() tea.Msg {
		pct, ok := <-ch
		if !ok {
			return voiceEngineClosedMsg{}
		}
		return voiceProgressMsg{pct: pct}
	}
}

func voiceTick(seq int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return voiceTickMsg{seq: seq} })
}

// voiceStartCmd starts the engine; the 5-minute ctx covers the first-use
// helper+model download.
func voiceStartCmd(eng voice.Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := eng.Start(ctx); err != nil {
			return voiceStartFailedMsg{err: err}
		}
		return voiceStartedMsg{}
	}
}

func voiceStopCmd(eng voice.Engine) tea.Cmd {
	return func() tea.Msg {
		if eng != nil {
			_ = eng.Stop()
		}
		return voiceStoppedMsg{}
	}
}

// toggleVoice flips the recording intent. Only one engine op runs at a time
// (voiceBusy); a toggle during an in-flight op just records the intent and
// the completion handler reconciles, so Start and Stop can never run out of
// order. When the setup is incomplete the hotkey opens the settings panel
// instead of starting (and failing) a recording.
func (a App) toggleVoice() (App, tea.Cmd) {
	if !a.voiceRec {
		a = a.ensureVoiceCfg()
		if !a.voiceReadyFn()(a.voiceCfg) {
			a.voiceSettingsView = newVoiceSettingsModel(a.db, a.masterKey, a.voiceCfg)
			a.voiceSettingsView.fromHotkey = true
			return a, nil
		}
		if a.voiceTest {
			// orphan test session (panel closed mid-test): its flush must
			// not be delivered as dictated text
			a.voiceTest = false
			a.voiceTestSeq++
			a.voiceSwallowFinal = true
		}
	}
	a.voiceRec = !a.voiceRec
	if a.aiView != nil {
		a.aiView.SetVoiceActive(a.voiceRec)
	}
	if !a.voiceRec {
		a.voicePartial = ""
	}
	if a.voiceBusy {
		return a, nil
	}
	if !a.voiceRec {
		a.voiceBusy = true
		return a, voiceStopCmd(a.voiceEngine)
	}
	var cmds []tea.Cmd
	var ensureCmd tea.Cmd
	a, ensureCmd = a.ensureVoice()
	cmds = append(cmds, ensureCmd)
	if a.voiceEngine == nil {
		a.voiceRec = false
		if a.aiView != nil {
			a.aiView.SetVoiceActive(false)
		}
		return a, tea.Batch(cmds...)
	}
	a.voiceBusy = true
	a.voiceStartedAt = time.Now()
	a.voiceTickSeq++
	cmds = append(cmds, voiceStartCmd(a.voiceEngine), voiceTick(a.voiceTickSeq))
	return a, tea.Batch(cmds...)
}

// stopVoice ends an active recording (lock path); the engine stays alive for
// the next toggle. Safe to call when idle.
func (a App) stopVoice() (App, tea.Cmd) {
	if !a.voiceRec {
		return a, nil
	}
	a.voiceRec = false
	a.voicePartial = ""
	if a.aiView != nil {
		a.aiView.SetVoiceActive(false)
	}
	if a.voiceBusy {
		// A start is in flight; its completion handler issues the stop.
		return a, nil
	}
	a.voiceBusy = true
	return a, voiceStopCmd(a.voiceEngine)
}

func (a App) handleVoiceEvent(msg voiceEventMsg) (App, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.ev.Type {
	case voice.EventPartial:
		if a.voiceTest {
			if a.voiceSettingsView != nil {
				a.voiceSettingsView.testPartial(msg.ev.Text)
			}
		} else {
			a.voicePartial = msg.ev.Text
		}
	case voice.EventFinal:
		a.voicePartial = ""
		if a.voiceTest {
			// test recording done: show the transcript, mark verified, stop
			a.voiceTest = false
			a.voiceTestSeq++
			a.voiceCfg.Verified = true
			if a.voiceSettingsView != nil {
				a.voiceSettingsView.testDone(msg.ev.Text)
			}
			var cmds []tea.Cmd
			if a.db != nil {
				if err := db.SetSetting(a.db, voiceVerifiedSettingKey, "1"); err != nil {
					e := err
					cmds = append(cmds, func() tea.Msg { return types.ErrorMsg{Err: e} })
				}
			}
			if !a.voiceBusy && a.voiceEngine != nil {
				a.voiceBusy = true
				cmds = append(cmds, voiceStopCmd(a.voiceEngine))
			}
			cmd = tea.Batch(cmds...)
		} else if a.voiceSwallowFinal {
			a.voiceSwallowFinal = false
		} else if strings.TrimSpace(msg.ev.Text) != "" {
			cmd = a.deliverVoiceText(msg.ev.Text)
		}
	case voice.EventDownloadProgress:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Downloading voice model %.0f%%", msg.ev.Pct), components.ToastInfo, 30*time.Second)
		cmd = tc
	case voice.EventState:
		// a cancelled test session ends with state idle (after any flushed
		// final); from here on finals are legitimate dictation again
		if a.voiceSwallowFinal && msg.ev.State == voice.StateIdle {
			a.voiceSwallowFinal = false
		}
	case voice.EventInfo:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.ev.Msg, components.ToastInfo, 10*time.Second)
		cmd = tc
	case voice.EventError:
		if a.voiceTest {
			a.voiceTest = false
			a.voiceTestSeq++
			if a.voiceSettingsView != nil {
				a.voiceSettingsView.testError(msg.ev.Msg)
			}
			if !a.voiceBusy && a.voiceEngine != nil {
				a.voiceBusy = true
				cmd = voiceStopCmd(a.voiceEngine)
			}
		} else {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Voice: "+msg.ev.Msg, components.ToastError, 6*time.Second)
			cmd = tc
		}
	}
	if a.voiceEngine != nil {
		return a, tea.Batch(cmd, waitVoiceEvent(a.voiceEngine.Events()))
	}
	return a, cmd
}

// handleVoiceTestRequest starts a settings-panel test recording (or cancels
// it when stop is set). The test reuses the shared engine; its events route
// to the panel instead of text delivery.
func (a App) handleVoiceTestRequest(msg voiceTestRequestMsg) (App, tea.Cmd) {
	if a.voiceTest || msg.stop {
		return a.endVoiceTest()
	}
	if a.voiceRec {
		// a test would hijack the live dictation event stream
		if a.voiceSettingsView != nil {
			a.voiceSettingsView.testError("stop dictation (ctrl+r) before running the test")
		}
		return a, nil
	}
	a = a.ensureVoiceCfg()
	if !a.voiceReadyFn()(a.voiceCfg) {
		if a.voiceSettingsView != nil {
			a.voiceSettingsView.testError("setup incomplete: " + voiceSetupIssue(a.voiceCfg, voice.ModelsRoot()))
		}
		return a, nil
	}
	var cmds []tea.Cmd
	var ensureCmd tea.Cmd
	a, ensureCmd = a.ensureVoice()
	cmds = append(cmds, ensureCmd)
	if a.voiceEngine == nil {
		return a, tea.Batch(cmds...)
	}
	a.voiceTest = true
	a.voiceBusy = true
	a.voiceTestSeq++
	if a.voiceSettingsView != nil {
		a.voiceSettingsView.testStarted()
	}
	cmds = append(cmds, voiceStartCmd(a.voiceEngine), voiceTestTick(a.voiceTestSeq))
	return a, tea.Batch(cmds...)
}

// endVoiceTest cancels the test recording intent. The final flushed by the
// stop is swallowed, not delivered as dictated text.
func (a App) endVoiceTest() (App, tea.Cmd) {
	if !a.voiceTest {
		return a, nil
	}
	a.voiceTest = false
	a.voiceTestSeq++
	a.voiceSwallowFinal = true
	if a.voiceSettingsView != nil {
		a.voiceSettingsView.testStopped()
	}
	if a.voiceBusy || a.voiceEngine == nil {
		// start still in flight; voiceStartedMsg reconciles into a stop
		return a, nil
	}
	a.voiceBusy = true
	return a, voiceStopCmd(a.voiceEngine)
}

// handleVoiceDownloadRequest runs the helper/model download in the
// background; progress streams back as voiceDownloadMsg.
func (a App) handleVoiceDownloadRequest(msg voiceDownloadRequestMsg) (App, tea.Cmd) {
	if a.voiceDlActive {
		return a, nil
	}
	a.voiceDlActive = true
	if a.voiceDlCh == nil {
		a.voiceDlCh = make(chan voiceDownloadMsg, 64)
	}
	fn := a.voiceDownload
	if fn == nil {
		fn = defaultVoiceDownload
	}
	ch := a.voiceDlCh
	target := msg.target
	go func() {
		err := fn(target, func(pct float64) {
			select {
			case ch <- voiceDownloadMsg{target: target, pct: pct}:
			default:
			}
		})
		ch <- voiceDownloadMsg{target: target, err: err, done: true}
	}()
	if a.voiceSettingsView != nil {
		a.voiceSettingsView.downloadStarted(target)
	}
	return a, waitVoiceDownload(ch)
}

func (a App) handleVoiceDownload(msg voiceDownloadMsg) (App, tea.Cmd) {
	if a.voiceSettingsView != nil {
		a.voiceSettingsView.downloadUpdate(msg)
	}
	if msg.done {
		a.voiceDlActive = false
	}
	return a, waitVoiceDownload(a.voiceDlCh)
}

// deliverVoiceText routes a finalized sentence: into the AI panel input when
// the overlay is open (enter submits), else typed into the active terminal.
// Finals in flight at lock time are dropped (Stop finalizes pending speech).
func (a App) deliverVoiceText(text string) tea.Cmd {
	if a.viewState != MainView {
		return nil
	}
	end := a.voiceCfg.SentenceEnd
	if a.aiVisible && a.aiView != nil {
		if end == voice.SentenceEndEnter {
			a.aiView.InsertText(text)
			return a.aiView.SubmitInput()
		}
		a.aiView.InsertText(end.Apply(text))
		return nil
	}
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) && isTerminalTab(a.tabs[a.activeTab].Type) {
		if m, ok := a.tabs[a.activeTab].Model.(interface{ PasteText(string) }); ok {
			m.PasteText(end.Apply(text))
		}
	}
	return nil
}

// withVoiceStatusHint prepends the recording indicator and partial preview.
func (a App) withVoiceStatusHint(hint string) string {
	if !a.voiceRec {
		return hint
	}
	rec := fmt.Sprintf("REC %ds", int(time.Since(a.voiceStartedAt).Seconds()))
	if a.voiceName != "" {
		rec += " " + a.voiceName
	}
	if p := strings.Join(strings.Fields(a.voicePartial), " "); p != "" {
		if r := []rune(p); len(r) > 30 {
			p = string(r[:30]) + "..."
		}
		rec += " · " + p
	}
	return rec + " · " + hint
}
