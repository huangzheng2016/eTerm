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
	"github.com/huangzheng2016/eTerm/internal/ui"
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
	voiceModelInt8SettingKey   = "voice_model_int8"
	voiceCustomModelSettingKey = "voice_custom_model"
	voiceVerifiedSettingKey    = "voice_verified"
	voiceParamsSettingPrefix   = "voice_params_"
	voiceVolcanoSettingKey     = "voice_volcano"

	voiceTestNoSpeechSecs = 5.0
)

type voiceSettings struct {
	Engine         string
	VADThreshold   float64
	VADSilenceMs   int
	SentenceEnd    voice.SentenceEnd
	Params         map[string]map[string]string
	ModelID        string
	ModelInt8      bool
	CustomModelDir string
	Verified       bool
}

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
		if newID, int8, legacy := voice.LegacyModelID(v); legacy {
			cfg.ModelID = newID
			cfg.ModelInt8 = int8
		} else if m := voice.ModelByID(v); m.ID == v {
			cfg.ModelID = v
		}
	}
	if v, err := db.GetSetting(database, voiceModelInt8SettingKey); err == nil && v == "1" {
		cfg.ModelInt8 = true
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
	int8 := "0"
	if cfg.ModelInt8 {
		int8 = "1"
	}
	if err := db.SetSetting(database, voiceModelInt8SettingKey, int8); err != nil {
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
				continue
			}
		}
		if err := persistEngineParams(database, mk, id, params); err != nil {
			return err
		}
	}
	return nil
}

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

var helperInstalledFn = voice.HelperInstalled

var latestHelperVersionFn = voice.LatestHelperVersion

func localModelTarget(cfg voiceSettings, modelsRoot string) (dir, kind string) {
	if cfg.CustomModelDir != "" {
		kind = voice.ModelKindSenseVoice
		if cfg.ModelInt8 && voice.HasBothPrecisions(cfg.CustomModelDir) {
			kind = voice.ModelKindSenseVoiceInt8
		}
		return cfg.CustomModelDir, kind
	}
	spec := voice.ModelByID(cfg.ModelID)
	kind = spec.Kind
	if cfg.ModelInt8 && spec.Kind == voice.ModelKindSenseVoice {
		kind = voice.ModelKindSenseVoiceInt8
	}
	return spec.ModelDir(modelsRoot), kind
}

func localModelReady(cfg voiceSettings, modelsRoot string) bool {
	if cfg.CustomModelDir != "" {
		return voice.ValidCustomModelDir(cfg.CustomModelDir)
	}
	return voice.ModelByID(cfg.ModelID).Installed(modelsRoot)
}

func voiceSetupReady(cfg voiceSettings, modelsRoot string) bool {
	return voiceSetupIssue(cfg, modelsRoot) == ""
}

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
		if cfg.CustomModelDir != "" && !voice.ValidCustomModelDir(cfg.CustomModelDir) {
			return "custom model path invalid (missing files)"
		}
		if !localModelReady(cfg, modelsRoot) {
			return "download a model first"
		}
	}
	return ""
}

func (a App) voiceReadyFn() func(voiceSettings) bool {
	if a.voiceReady != nil {
		return a.voiceReady
	}
	return func(cfg voiceSettings) bool { return voiceSetupReady(cfg, voice.ModelsRoot()) }
}

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

func (a App) toggleVoice() (App, tea.Cmd) {
	if !a.voiceRec {
		a = a.ensureVoiceCfg()
		if !a.voiceReadyFn()(a.voiceCfg) {
			a.voiceSettingsView = newVoiceSettingsModel(a.db, a.masterKey, a.voiceCfg)
			a.voiceSettingsView.fromHotkey = true
			return a, nil
		}
		if a.voiceTest {
			a.voiceTest = false
			a.voiceTestSeq++
			a.voiceSwallowFinal = true
		}
	}
	a.voiceRec = !a.voiceRec
	if a.aiView != nil {
		a.aiView.SetVoiceActive(a.voiceRec)
	}
	if a.voiceRec {
		a.voiceDropNotified = false
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
	_ = a.voiceEngine.SetVAD(a.voiceCfg.vadParams())
	cmds = append(cmds, voiceStartCmd(a.voiceEngine), voiceTick(a.voiceTickSeq))
	return a, tea.Batch(cmds...)
}

func (a App) stopVoice() (App, tea.Cmd) {
	if !a.voiceRec {
		return a, nil
	}
	a.voiceRec = false
	if a.aiView != nil {
		a.aiView.SetVoiceActive(false)
	}
	if a.voiceBusy {
		return a, nil
	}
	a.voiceBusy = true
	return a, voiceStopCmd(a.voiceEngine)
}

func (a App) handleVoiceEvent(msg voiceEventMsg) (App, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.ev.Type {
	case voice.EventPartial:
		if a.voiceTest && a.voiceSettingsView != nil {
			a.voiceSettingsView.testPartial(msg.ev.Text)
		}
	case voice.EventFinal:
		if a.voiceTest {
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
			a, cmd = a.deliverVoiceText(msg.ev.Text)
		}
	case voice.EventDownloadProgress:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Downloading voice model %.0f%%", msg.ev.Pct), components.ToastInfo, 30*time.Second)
		cmd = tc
	case voice.EventState:
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

func (a App) handleVoiceTestRequest(msg voiceTestRequestMsg) (App, tea.Cmd) {
	if a.voiceTest || msg.stop {
		return a.endVoiceTest()
	}
	if a.voiceRec {
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
	p := a.voiceCfg.vadParams()
	p.NoSpeechTimeout = voiceTestNoSpeechSecs
	_ = a.voiceEngine.SetVAD(p)
	cmds = append(cmds, voiceStartCmd(a.voiceEngine), voiceTestTick(a.voiceTestSeq))
	return a, tea.Batch(cmds...)
}

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
		return a, nil
	}
	a.voiceBusy = true
	return a, voiceStopCmd(a.voiceEngine)
}

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

func (a App) deliverVoiceText(text string) (App, tea.Cmd) {
	if a.viewState != MainView {
		return a, nil
	}
	end := a.voiceCfg.SentenceEnd
	if a.aiVisible && a.aiView != nil {
		if end == voice.SentenceEndEnter {
			a.aiView.InsertText(text)
			return a, a.aiView.SubmitInput()
		}
		a.aiView.InsertText(end.Apply(text))
		return a, nil
	}
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) && isTerminalTab(a.tabs[a.activeTab].Type) {
		if m, ok := a.tabs[a.activeTab].Model.(interface{ PasteText(string) }); ok {
			m.PasteText(end.Apply(text))
		}
		return a, nil
	}
	if a.voiceDropNotified {
		return a, nil
	}
	a.voiceDropNotified = true
	t, cmd := a.toast.Show("Voice: no active terminal tab", components.ToastInfo, 4*time.Second)
	a.toast = t
	return a, cmd
}

func (a App) withVoiceStatusHint(hint string) string {
	if !a.voiceRec {
		return hint
	}
	return ui.ErrorStyle.Render("REC") + " · " + hint
}
