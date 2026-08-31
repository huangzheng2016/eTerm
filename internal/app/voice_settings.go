package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"github.com/huangzheng2016/eTerm/internal/voice"
)

const (
	voiceRowEngine = iota
	voiceRowHelper
	voiceRowModel0
	voiceRowModel1
	voiceRowModel2
	voiceRowTest
	voiceRowThreshold
	voiceRowSilence
	voiceRowSentenceEnd
	voiceRowAPIKey
	voiceRowAppKey
	voiceRowAccessKey
	voiceRowCount
)

// voiceHelperTarget is the download target id for the helper binary; model
// downloads use the catalog model ID.
const voiceHelperTarget = "helper"

// voiceSettingsModel is the voice input settings overlay (opened from the
// command palette, the esc menu, or by ctrl+r while the setup is
// incomplete). It guides the local setup: helper download, model
// download/selection, and a test recording.
type voiceSettingsModel struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	cfg    voiceSettings
	cursor int
	edit   int // editing key row, -1 when not editing
	input  textinput.Model

	modelsRoot        string
	helperInstalledFn func() bool // test hook; nil = voice.HelperInstalled
	helperOK          bool
	modelOK           []bool

	dlTarget    string // "" when no download is running
	dlPct       float64
	dlErr       string
	dlErrTarget string

	testing  bool
	testText string // partial while recording, final when done
	testErr  string
}

func newVoiceSettingsModel(database *gorm.DB, mk *security.MasterKeyManager, cfg voiceSettings) *voiceSettingsModel {
	ti := textinput.New()
	ti.CharLimit = 256
	m := &voiceSettingsModel{db: database, mk: mk, cfg: cfg, edit: -1, input: ti, modelsRoot: voice.ModelsRoot()}
	m.refreshInstallState()
	return m
}

func (m *voiceSettingsModel) refreshInstallState() {
	installed := m.helperInstalledFn
	if installed == nil {
		installed = helperInstalledFn
	}
	m.helperOK = installed()
	catalog := voice.ModelCatalog()
	m.modelOK = make([]bool, len(catalog))
	for i, spec := range catalog {
		m.modelOK[i] = spec.Installed(m.modelsRoot)
	}
}

func (m *voiceSettingsModel) keyValue(row int) string {
	switch row {
	case voiceRowAPIKey:
		return m.cfg.VolcanoAPIKey
	case voiceRowAppKey:
		return m.cfg.VolcanoAppKey
	case voiceRowAccessKey:
		return m.cfg.VolcanoAccessKey
	}
	return ""
}

func (m *voiceSettingsModel) persist(keepEngine bool) tea.Cmd {
	database := m.db
	mk := m.mk
	cfg := m.cfg
	return func() tea.Msg {
		if err := persistVoiceSettings(database, mk, cfg); err != nil {
			return types.ErrorMsg{Err: err}
		}
		return voiceSettingsChangedMsg{cfg: cfg, keepEngine: keepEngine}
	}
}

// adjust cycles the enum rows and steps the numeric rows; returns the
// persist command when the row changed. Engine changes rebuild the engine,
// VAD changes apply live via SetVAD.
func (m *voiceSettingsModel) adjust(dir int) tea.Cmd {
	switch m.cursor {
	case voiceRowEngine:
		if m.cfg.Engine == voiceEngineLocal {
			m.cfg.Engine = voiceEngineVolcano
		} else {
			m.cfg.Engine = voiceEngineLocal
		}
		m.cfg.Verified = false
		return m.persist(false)
	case voiceRowThreshold:
		m.cfg.VADThreshold = math.Round((m.cfg.VADThreshold+float64(dir)*0.05)*100) / 100
		if m.cfg.VADThreshold < 0 {
			m.cfg.VADThreshold = 0
		}
		if m.cfg.VADThreshold > 1 {
			m.cfg.VADThreshold = 1
		}
		return m.persist(true)
	case voiceRowSilence:
		m.cfg.VADSilenceMs += dir * 50
		if m.cfg.VADSilenceMs < 50 {
			m.cfg.VADSilenceMs = 50
		}
		if m.cfg.VADSilenceMs > 5000 {
			m.cfg.VADSilenceMs = 5000
		}
		return m.persist(true)
	case voiceRowSentenceEnd:
		if m.cfg.SentenceEnd == voice.SentenceEndEnter {
			m.cfg.SentenceEnd = voice.SentenceEndSpace
		} else {
			m.cfg.SentenceEnd = voice.SentenceEndEnter
		}
		return m.persist(true)
	}
	return nil
}

// startDownload requests a helper or model download; one download runs at a
// time.
func (m *voiceSettingsModel) startDownload(target string) tea.Cmd {
	if m.dlTarget != "" {
		return nil
	}
	if target == voiceHelperTarget && m.helperOK {
		return nil
	}
	m.dlErr = ""
	m.dlErrTarget = ""
	return func() tea.Msg { return voiceDownloadRequestMsg{target: target} }
}

// modelAction selects an installed model (persisted) or starts its download.
func (m *voiceSettingsModel) modelAction(i int) tea.Cmd {
	spec := voice.ModelCatalog()[i]
	if m.modelOK[i] {
		if m.cfg.ModelID == spec.ID {
			return nil
		}
		m.cfg.ModelID = spec.ID
		m.cfg.Verified = false
		m.testText = ""
		m.testErr = ""
		return m.persist(true)
	}
	return m.startDownload(spec.ID)
}

func (m *voiceSettingsModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	if m.edit >= 0 {
		switch msg.String() {
		case "esc", "escape":
			m.edit = -1
			m.input.Blur()
			return false, nil
		case "enter":
			v := strings.TrimSpace(m.input.Value())
			switch m.edit {
			case voiceRowAPIKey:
				m.cfg.VolcanoAPIKey = v
			case voiceRowAppKey:
				m.cfg.VolcanoAppKey = v
			case voiceRowAccessKey:
				m.cfg.VolcanoAccessKey = v
			}
			m.edit = -1
			m.input.Blur()
			return false, m.persist(false)
		}
		m.input, cmd = m.input.Update(msg)
		return false, cmd
	}
	switch msg.String() {
	case "esc", "escape":
		return true, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < voiceRowCount-1 {
			m.cursor++
		}
	case "left", "h":
		return false, m.adjust(-1)
	case "right", "l", " ":
		return false, m.adjust(1)
	case "enter":
		switch {
		case m.cursor == voiceRowHelper:
			return false, m.startDownload(voiceHelperTarget)
		case m.cursor >= voiceRowModel0 && m.cursor <= voiceRowModel2:
			return false, m.modelAction(m.cursor - voiceRowModel0)
		case m.cursor == voiceRowTest:
			if m.testing {
				return false, func() tea.Msg { return voiceTestRequestMsg{stop: true} }
			}
			m.testText = ""
			m.testErr = ""
			return false, func() tea.Msg { return voiceTestRequestMsg{} }
		case m.cursor >= voiceRowAPIKey:
			m.edit = m.cursor
			m.input.SetValue(m.keyValue(m.cursor))
			m.input.SetWidth(40)
			return false, m.input.Focus()
		}
		return false, m.adjust(1)
	}
	return false, nil
}

func (m *voiceSettingsModel) paste(msg tea.PasteMsg) {
	if m.edit >= 0 {
		m.input = inputpaste.TextInput(m.input, msg)
	}
}

// downloadStarted marks a download in flight (the app accepted the request).
func (m *voiceSettingsModel) downloadStarted(target string) {
	m.dlTarget = target
	m.dlPct = 0
}

// downloadUpdate applies one progress/done event; failures stay visible on
// the affected row until the next attempt.
func (m *voiceSettingsModel) downloadUpdate(msg voiceDownloadMsg) {
	if !msg.done {
		if m.dlTarget == msg.target {
			m.dlPct = msg.pct
		}
		return
	}
	m.dlTarget = ""
	if msg.err != nil {
		m.dlErr = msg.err.Error()
		m.dlErrTarget = msg.target
	} else {
		m.dlErr = ""
		m.dlErrTarget = ""
	}
	m.refreshInstallState()
}

func (m *voiceSettingsModel) testStarted() {
	m.testing = true
	m.testText = ""
	m.testErr = ""
}

func (m *voiceSettingsModel) testPartial(text string) { m.testText = text }

func (m *voiceSettingsModel) testDone(text string) {
	m.testing = false
	m.testText = text
	m.testErr = ""
	m.cfg.Verified = true
}

func (m *voiceSettingsModel) testError(err string) {
	m.testing = false
	m.testErr = err
}

func (m *voiceSettingsModel) testStopped() {
	m.testing = false
	if m.testText == "" && m.testErr == "" {
		m.testErr = "no speech detected"
	}
}

func maskVoiceKey(v string) string {
	if v == "" {
		return "(not set)"
	}
	return "(set)"
}

func (m *voiceSettingsModel) helperValue() string {
	switch {
	case m.dlTarget == voiceHelperTarget:
		return fmt.Sprintf("downloading %.0f%%", m.dlPct)
	case m.dlErrTarget == voiceHelperTarget && m.dlErr != "":
		return "failed: " + m.dlErr
	case m.helperOK:
		return "installed"
	}
	return "missing - enter to download"
}

func (m *voiceSettingsModel) modelValue(i int) string {
	spec := voice.ModelCatalog()[i]
	value := "not downloaded (" + spec.Size + ")"
	switch {
	case m.dlTarget == spec.ID:
		value = fmt.Sprintf("downloading %.0f%%", m.dlPct)
	case m.dlErrTarget == spec.ID && m.dlErr != "":
		value = "failed: " + m.dlErr
	case m.modelOK[i]:
		value = "installed"
	}
	if m.cfg.ModelID == spec.ID {
		value = "[active] " + value
	}
	return value
}

func (m *voiceSettingsModel) testValue() string {
	shorten := func(s string) string {
		if r := []rune(s); len(r) > 40 {
			return string(r[:40]) + "..."
		}
		return s
	}
	switch {
	case m.testing:
		if m.testText != "" {
			return "recording... " + shorten(m.testText)
		}
		return "recording... speak now (enter stops)"
	case m.testErr != "":
		return "failed: " + shorten(m.testErr)
	case m.testText != "":
		return "heard: " + shorten(m.testText)
	case m.cfg.Verified:
		return "verified - enter to test again"
	}
	return "enter to record a sample"
}

// statusLine summarizes the readiness gate (what ctrl+r does next).
func (m *voiceSettingsModel) statusLine() string {
	if m.cfg.Engine == voiceEngineVolcano {
		if m.cfg.VolcanoAPIKey != "" && m.cfg.VolcanoAppKey != "" && m.cfg.VolcanoAccessKey != "" {
			return "ready - ctrl+r starts recording"
		}
		return "setup incomplete - enter the Volcano API keys"
	}
	activeInstalled := false
	for i, spec := range voice.ModelCatalog() {
		if spec.ID == m.cfg.ModelID && m.modelOK[i] {
			activeInstalled = true
		}
	}
	switch {
	case !m.helperOK:
		return "setup incomplete - download the helper binary, then a model"
	case !activeInstalled:
		return "setup incomplete - download the active model"
	}
	return "ready - ctrl+r starts recording"
}

func (m *voiceSettingsModel) View() string {
	threshold := fmt.Sprintf("%.2f", m.cfg.VADThreshold)
	if m.cfg.VADThreshold == 0 {
		threshold = "default"
	}
	rows := []struct {
		label string
		value string
	}{
		{"Engine", m.cfg.Engine},
		{"Helper binary", m.helperValue()},
		{voice.ModelCatalog()[0].Name, m.modelValue(0)},
		{voice.ModelCatalog()[1].Name, m.modelValue(1)},
		{voice.ModelCatalog()[2].Name, m.modelValue(2)},
		{"Microphone test", m.testValue()},
		{"speech sensitivity (0-1)", threshold},
		{"end-of-sentence silence (ms)", strconv.Itoa(m.cfg.VADSilenceMs)},
		{"Sentence end", string(m.cfg.SentenceEnd)},
		{"Volcano API key", maskVoiceKey(m.cfg.VolcanoAPIKey)},
		{"Volcano App key", maskVoiceKey(m.cfg.VolcanoAppKey)},
		{"Volcano Access key", maskVoiceKey(m.cfg.VolcanoAccessKey)},
	}
	var lines []string
	lines = append(lines, ui.TitleStyle.Render("Voice Input"), "")
	for i, r := range rows {
		cursor := "  "
		style := ui.DimStyle
		if i == m.cursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		value := r.value
		if m.edit == i {
			value = m.input.View()
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", cursor, style.Render(fmt.Sprintf("%-28s", r.label)), ui.DimStyle.Render(value)))
	}
	lines = append(lines,
		"",
		ui.DimStyle.Render(m.statusLine()),
		ui.DimStyle.Render("up/down move · left/right change · enter select/edit · esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}
