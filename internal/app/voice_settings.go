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

// voice row kinds; the visible row list is built per state (rows()).
const (
	vrowEngine = iota
	vrowHelper
	vrowModels
	vrowTest
	vrowThreshold
	vrowSilence
	vrowSentenceEnd
	vrowParam
	vrowBack
	vrowModel
	vrowCustomPath
)

// panel views: the main settings list and the model catalog submenu.
const (
	voiceViewMain = iota
	voiceViewModels
)

// voiceHelperTarget is the download target id for the helper binary; model
// downloads use the catalog model ID.
const voiceHelperTarget = "helper"

// voiceRow is one rendered settings row; param/modelIdx qualify the kind.
type voiceRow struct {
	kind     int
	param    voice.ParamSpec // vrowParam
	modelIdx int             // vrowModel
}

// voiceSettingsModel is the voice input settings overlay (opened from the
// command palette, the esc menu, or by ctrl+r while the setup is
// incomplete). Rows render per the selected engine's descriptor; the local
// engine additionally shows the helper row and the Model submenu.
type voiceSettingsModel struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	cfg    voiceSettings
	cursor int
	view   int // voiceView*
	edit   int // editing row index, -1 when not editing
	input  textinput.Model

	modelsRoot        string
	helperInstalledFn func() bool // test hook; nil = voice.HelperInstalled
	helperOK          bool
	modelOK           []bool

	dlTarget    string // "" when no download is running
	dlPct       float64
	dlErr       string
	dlErrTarget string

	customErr string // invalid custom model path, shown on the row

	fromHotkey bool // opened by ctrl+r with an incomplete setup: show the reason

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

// rows builds the visible row list for the current view and engine.
func (m *voiceSettingsModel) rows() []voiceRow {
	if m.view == voiceViewModels {
		rows := []voiceRow{{kind: vrowBack}}
		for i := range voice.ModelCatalog() {
			rows = append(rows, voiceRow{kind: vrowModel, modelIdx: i})
		}
		return append(rows, voiceRow{kind: vrowCustomPath})
	}
	rows := []voiceRow{{kind: vrowEngine}}
	if m.cfg.Engine == voiceEngineLocal {
		rows = append(rows, voiceRow{kind: vrowHelper}, voiceRow{kind: vrowModels})
	}
	rows = append(rows,
		voiceRow{kind: vrowTest},
		voiceRow{kind: vrowThreshold},
		voiceRow{kind: vrowSilence},
		voiceRow{kind: vrowSentenceEnd},
	)
	if d, ok := voice.EngineDescriptorByID(m.cfg.Engine); ok {
		for _, p := range d.Params {
			rows = append(rows, voiceRow{kind: vrowParam, param: p})
		}
	}
	return rows
}

// rowCount is the visible row count (mouse hit-testing).
func (m *voiceSettingsModel) rowCount() int { return len(m.rows()) }

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
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	switch rows[m.cursor].kind {
	case vrowEngine:
		descs := voice.EngineDescriptors()
		if len(descs) == 0 {
			return nil
		}
		idx := -1
		for i, d := range descs {
			if d.ID == m.cfg.Engine {
				idx = i
				break
			}
		}
		idx = (idx + dir + len(descs)) % len(descs)
		m.cfg.Engine = descs[idx].ID
		m.cfg.Verified = false
		return m.persist(false)
	case vrowThreshold:
		m.cfg.VADThreshold = math.Round((m.cfg.VADThreshold+float64(dir)*0.05)*100) / 100
		if m.cfg.VADThreshold < 0 {
			m.cfg.VADThreshold = 0
		}
		if m.cfg.VADThreshold > 1 {
			m.cfg.VADThreshold = 1
		}
		return m.persist(true)
	case vrowSilence:
		m.cfg.VADSilenceMs += dir * 50
		if m.cfg.VADSilenceMs < 50 {
			m.cfg.VADSilenceMs = 50
		}
		if m.cfg.VADSilenceMs > 5000 {
			m.cfg.VADSilenceMs = 5000
		}
		return m.persist(true)
	case vrowSentenceEnd:
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

// modelAction selects an installed model (persisted, clearing any custom
// path) or starts its download.
func (m *voiceSettingsModel) modelAction(i int) tea.Cmd {
	spec := voice.ModelCatalog()[i]
	if m.modelOK[i] {
		if m.cfg.ModelID == spec.ID && m.cfg.CustomModelDir == "" {
			return nil
		}
		m.cfg.ModelID = spec.ID
		m.cfg.CustomModelDir = ""
		m.customErr = ""
		m.cfg.Verified = false
		m.testText = ""
		m.testErr = ""
		return m.persist(true)
	}
	return m.startDownload(spec.ID)
}

func (m *voiceSettingsModel) enterModels() {
	m.view = voiceViewModels
	m.cursor = 0
	m.customErr = ""
}

func (m *voiceSettingsModel) leaveModels() {
	m.view = voiceViewMain
	m.cursor = 0
	for i, r := range m.rows() {
		if r.kind == vrowModels {
			m.cursor = i
		}
	}
}

// commitEdit applies the edited value: engine params persist encrypted and
// rebuild the engine; the custom model path is validated before persisting
// (empty clears it) and applies live via set_model.
func (m *voiceSettingsModel) commitEdit(rows []voiceRow) tea.Cmd {
	if m.edit < 0 || m.edit >= len(rows) {
		m.edit = -1
		return nil
	}
	r := rows[m.edit]
	v := strings.TrimSpace(m.input.Value())
	m.edit = -1
	m.input.Blur()
	switch r.kind {
	case vrowParam:
		m.cfg.setEngineParam(m.cfg.Engine, r.param.Key, v)
		return m.persist(false)
	case vrowCustomPath:
		if v == "" {
			m.cfg.CustomModelDir = ""
			m.customErr = ""
			m.cfg.Verified = false
			return m.persist(true)
		}
		if !voice.ValidCustomModelDir(v) {
			m.customErr = "needs tokens.txt and model.onnx or model.int8.onnx"
			return nil
		}
		m.cfg.CustomModelDir = v
		m.customErr = ""
		m.cfg.Verified = false
		m.testText = ""
		m.testErr = ""
		return m.persist(true)
	}
	return nil
}

func (m *voiceSettingsModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	rows := m.rows()
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.edit >= 0 {
		switch msg.String() {
		case "esc", "escape":
			m.edit = -1
			m.input.Blur()
			return false, nil
		case "enter":
			return false, m.commitEdit(rows)
		}
		m.input, cmd = m.input.Update(msg)
		return false, cmd
	}
	switch msg.String() {
	case "esc", "escape":
		if m.view == voiceViewModels {
			m.leaveModels()
			return false, nil
		}
		return true, nil
	case "left", "h":
		if m.view == voiceViewModels {
			m.leaveModels()
			return false, nil
		}
		return false, m.adjust(-1)
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "right", "l", " ":
		if m.view == voiceViewMain && rows[m.cursor].kind == vrowModels {
			m.enterModels()
			return false, nil
		}
		return false, m.adjust(1)
	case "enter":
		switch rows[m.cursor].kind {
		case vrowHelper:
			return false, m.startDownload(voiceHelperTarget)
		case vrowModels:
			m.enterModels()
			return false, nil
		case vrowTest:
			if m.testing {
				return false, func() tea.Msg { return voiceTestRequestMsg{stop: true} }
			}
			m.testText = ""
			m.testErr = ""
			return false, func() tea.Msg { return voiceTestRequestMsg{} }
		case vrowBack:
			m.leaveModels()
			return false, nil
		case vrowModel:
			return false, m.modelAction(rows[m.cursor].modelIdx)
		case vrowParam:
			m.edit = m.cursor
			m.input.SetValue(m.cfg.engineParams(m.cfg.Engine)[rows[m.cursor].param.Key])
			m.input.SetWidth(40)
			return false, m.input.Focus()
		case vrowCustomPath:
			m.edit = m.cursor
			m.customErr = ""
			m.input.SetValue(m.cfg.CustomModelDir)
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
	if m.cfg.ModelID == spec.ID && m.cfg.CustomModelDir == "" {
		value = "[active] " + value
	}
	return value
}

func (m *voiceSettingsModel) customValue() string {
	if m.customErr != "" {
		return "invalid: " + m.customErr
	}
	if m.cfg.CustomModelDir == "" {
		return "(not set) - enter to edit"
	}
	if !voice.ValidCustomModelDir(m.cfg.CustomModelDir) {
		return m.cfg.CustomModelDir + " (missing files)"
	}
	return "[active] " + m.cfg.CustomModelDir
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
	if issue := voiceSetupIssue(m.cfg, m.modelsRoot); issue != "" {
		return "setup incomplete - " + issue
	}
	return "ready - ctrl+r starts recording"
}

// noticeText is the top-of-panel hint shown when ctrl+r routed here because
// the setup is incomplete; it names the missing piece and clears itself once
// the setup becomes ready while the panel is open.
func (m *voiceSettingsModel) noticeText() string {
	if !m.fromHotkey {
		return ""
	}
	if issue := voiceSetupIssue(m.cfg, m.modelsRoot); issue != "" {
		return "voice input is not set up yet: " + issue + " below"
	}
	return ""
}

func (m *voiceSettingsModel) rowText(r voiceRow, threshold string) (label, value string) {
	switch r.kind {
	case vrowEngine:
		label = "Engine"
		if d, ok := voice.EngineDescriptorByID(m.cfg.Engine); ok {
			value = d.Label
		} else {
			value = m.cfg.Engine + " (unknown)"
		}
	case vrowHelper:
		label, value = "Helper binary", m.helperValue()
	case vrowModels:
		label = "Model >"
		switch {
		case m.dlTarget != "" && m.dlTarget != voiceHelperTarget:
			value = fmt.Sprintf("downloading %.0f%%", m.dlPct)
		case m.dlErrTarget != "" && m.dlErrTarget != voiceHelperTarget && m.dlErr != "":
			value = "failed: " + m.dlErr
		case m.cfg.CustomModelDir != "":
			value = "custom path"
		default:
			value = voice.ModelByID(m.cfg.ModelID).Name
		}
	case vrowTest:
		label, value = "Microphone test", m.testValue()
	case vrowThreshold:
		label, value = "speech sensitivity (0-1)", threshold
	case vrowSilence:
		label, value = "end-of-sentence silence (ms)", strconv.Itoa(m.cfg.VADSilenceMs)
	case vrowSentenceEnd:
		label, value = "Sentence end", string(m.cfg.SentenceEnd)
	case vrowParam:
		label = r.param.Label
		v := m.cfg.engineParams(m.cfg.Engine)[r.param.Key]
		if r.param.Secret {
			value = maskVoiceKey(v)
		} else if v == "" {
			value = "(not set)"
		} else {
			value = v
		}
	case vrowBack:
		label, value = "Back", "<"
	case vrowModel:
		label, value = voice.ModelCatalog()[r.modelIdx].Name, m.modelValue(r.modelIdx)
	case vrowCustomPath:
		label, value = "Custom model path", m.customValue()
	}
	return
}

func (m *voiceSettingsModel) View() string {
	rows := m.rows()
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	threshold := fmt.Sprintf("%.2f", m.cfg.VADThreshold)
	if m.cfg.VADThreshold == 0 {
		threshold = "default"
	}
	title := "Voice Input"
	hint := "up/down move · left/right change · enter select/edit · esc close"
	if m.view == voiceViewModels {
		title = "Voice Input - Model"
		hint = "up/down move · enter select/download · esc back"
	}
	var lines []string
	lines = append(lines, ui.TitleStyle.Render(title), "")
	if notice := m.noticeText(); notice != "" {
		lines = append(lines, ui.DimStyle.Render(notice), "")
	}
	for i, r := range rows {
		cursor := "  "
		style := ui.DimStyle
		if i == m.cursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		label, value := m.rowText(r, threshold)
		if m.edit == i {
			value = m.input.View()
		}
		lines = append(lines, fmt.Sprintf("%s%s %s", cursor, style.Render(fmt.Sprintf("%-28s", label)), ui.DimStyle.Render(value)))
	}
	lines = append(lines, "")
	if m.view == voiceViewMain {
		lines = append(lines, ui.DimStyle.Render(m.statusLine()))
	}
	lines = append(lines, ui.DimStyle.Render(hint))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}
