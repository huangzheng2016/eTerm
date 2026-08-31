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
	voiceVolcanoSettingKey     = "voice_volcano" // encrypted JSON {api_key, app_key, access_key}
)

// voiceSettings holds the voice input configuration. VADThreshold 0 keeps the
// engine default.
type voiceSettings struct {
	Engine           string
	VADThreshold     float64
	VADSilenceMs     int // min silence to end an utterance, milliseconds
	SentenceEnd      voice.SentenceEnd
	VolcanoAPIKey    string
	VolcanoAppKey    string
	VolcanoAccessKey string
}

func defaultVoiceSettings() voiceSettings {
	return voiceSettings{Engine: voiceEngineLocal, VADSilenceMs: 1000, SentenceEnd: voice.SentenceEndSpace}
}

func (cfg voiceSettings) vadParams() voice.VADParams {
	return voice.VADParams{
		Threshold:  cfg.VADThreshold,
		MinSilence: float64(cfg.VADSilenceMs) / 1000,
	}
}

func loadVoiceSettings(database *gorm.DB, mk *security.MasterKeyManager) voiceSettings {
	cfg := defaultVoiceSettings()
	if database == nil {
		return cfg
	}
	if v, err := db.GetSetting(database, voiceEngineSettingKey); err == nil {
		switch v {
		case voiceEngineLocal, voiceEngineVolcano:
			cfg.Engine = v
		}
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
	if enc, err := db.GetSetting(database, voiceVolcanoSettingKey); err == nil && enc != "" && mk != nil {
		if k := mk.GetKey(); k != nil {
			plain, err := security.Decrypt(enc, k.Bytes())
			k.Clear()
			if err == nil {
				var keys struct {
					APIKey    string `json:"api_key"`
					AppKey    string `json:"app_key"`
					AccessKey string `json:"access_key"`
				}
				if json.Unmarshal(plain, &keys) == nil {
					cfg.VolcanoAPIKey = keys.APIKey
					cfg.VolcanoAppKey = keys.AppKey
					cfg.VolcanoAccessKey = keys.AccessKey
				}
			}
		}
	}
	return cfg
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
	data, err := json.Marshal(map[string]string{
		"api_key":    cfg.VolcanoAPIKey,
		"app_key":    cfg.VolcanoAppKey,
		"access_key": cfg.VolcanoAccessKey,
	})
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
	return db.SetSetting(database, voiceVolcanoSettingKey, enc)
}

// defaultVoiceEngine builds the configured engine; onProgress reports the
// helper binary download (the model download arrives as engine events).
// Volcano composes a passthrough helper (capture+VAD) with the cloud client.
func defaultVoiceEngine(cfg voiceSettings, onProgress func(float64)) (voice.Engine, error) {
	if cfg.Engine == voiceEngineVolcano {
		return voice.NewVolcanoFeedEngine(voice.VolcanoFeedConfig{
			Volcano: voice.VolcanoConfig{
				APIKey:    cfg.VolcanoAPIKey,
				AppKey:    cfg.VolcanoAppKey,
				AccessKey: cfg.VolcanoAccessKey,
			},
			Helper: voice.LocalConfig{
				VAD:                cfg.vadParams(),
				OnDownloadProgress: onProgress,
			},
		}), nil
	}
	return voice.NewLocalEngine(voice.LocalConfig{
		VAD:                cfg.vadParams(),
		OnDownloadProgress: onProgress,
	}), nil
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
// order.
func (a App) toggleVoice() (App, tea.Cmd) {
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
		a.voicePartial = msg.ev.Text
	case voice.EventFinal:
		a.voicePartial = ""
		if strings.TrimSpace(msg.ev.Text) != "" {
			cmd = a.deliverVoiceText(msg.ev.Text)
		}
	case voice.EventDownloadProgress:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Downloading voice model %.0f%%", msg.ev.Pct), components.ToastInfo, 30*time.Second)
		cmd = tc
	case voice.EventError:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("Voice: "+msg.ev.Msg, components.ToastError, 6*time.Second)
		cmd = tc
	}
	if a.voiceEngine != nil {
		return a, tea.Batch(cmd, waitVoiceEvent(a.voiceEngine.Events()))
	}
	return a, cmd
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
