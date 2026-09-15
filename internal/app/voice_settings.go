package app

import (
	"fmt"
	"math"
	"reflect"
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
	vrowHelper = iota
	vrowTest
	vrowThreshold
	vrowSilence
	vrowSentenceEnd
	vrowContext
	vrowDDC
	vrowParam
	vrowModel
	vrowCustomPath
	vrowPrecision
	vrowEngineOption
)

const voiceHelperTarget = "helper"

var (
	voiceCatStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	voiceLabelStyle    = lipgloss.NewStyle().Width(24)
	voiceKeyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
	voiceSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	voiceDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	voiceCaptureStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)
	voiceHintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
	voiceErrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
)

type voiceRow struct {
	kind      int
	param     voice.ParamSpec
	modelIdx  int
	engineIdx int
}

type voiceSection struct {
	title string
	rows  []voiceRow
}

type voiceSettingsModel struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	cfg    voiceSettings
	saved  voiceSettings
	cursor int
	scroll int
	width  int
	height int
	edit   int
	input  textinput.Model

	modified bool

	modelsRoot        string
	helperInstalledFn func() bool
	helperVersionFn   func() string
	helperOK          bool
	helperVersion     string
	modelOK           []bool

	checkingUpdate bool
	updateChecked  bool
	updateTag      string
	updateErr      string

	dlTarget    string
	dlPct       float64
	dlErr       string
	dlErrTarget string

	customErr string

	fromHotkey bool

	testing  bool
	testText string
	testErr  string
}

func (a App) voiceTab() *voiceSettingsModel {
	for i := range a.tabs {
		if a.tabs[i].Type != VoiceTab {
			continue
		}
		if m, ok := a.tabs[i].Model.(*voiceSettingsModel); ok {
			return m
		}
	}
	return nil
}

func (a App) activeTabIsVoiceSettings() bool {
	if a.viewState != MainView || a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	return a.tabs[a.activeTab].Type == VoiceTab
}

func cloneVoiceSettings(cfg voiceSettings) voiceSettings {
	out := cfg
	if cfg.Params != nil {
		out.Params = make(map[string]map[string]string, len(cfg.Params))
		for id, params := range cfg.Params {
			p := make(map[string]string, len(params))
			for k, v := range params {
				p[k] = v
			}
			out.Params[id] = p
		}
	}
	return out
}

func newVoiceSettingsModel(database *gorm.DB, mk *security.MasterKeyManager, cfg voiceSettings) *voiceSettingsModel {
	ti := textinput.New()
	ti.CharLimit = 256
	staged := cloneVoiceSettings(cfg)
	m := &voiceSettingsModel{
		db:         database,
		mk:         mk,
		cfg:        staged,
		saved:      cloneVoiceSettings(cfg),
		edit:       -1,
		input:      ti,
		modelsRoot: voice.ModelsRoot(),
	}
	m.refreshInstallState()
	return m
}

func (m *voiceSettingsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *voiceSettingsModel) Init() tea.Cmd { return nil }

func (m *voiceSettingsModel) refreshInstallState() {
	installed := m.helperInstalledFn
	if installed == nil {
		installed = helperInstalledFn
	}
	m.helperOK = installed()
	m.helperVersion = ""
	if m.helperOK {
		vfn := m.helperVersionFn
		if vfn == nil {
			vfn = voice.HelperVersion
		}
		m.helperVersion = vfn()
	}
	m.checkingUpdate = false
	m.updateChecked = false
	m.updateTag = ""
	m.updateErr = ""
	catalog := voice.ModelCatalog()
	m.modelOK = make([]bool, len(catalog))
	for i, spec := range catalog {
		m.modelOK[i] = spec.Installed(m.modelsRoot)
	}
}

func enginePickerDescriptors() []voice.EngineDescriptor {
	descs := voice.EngineDescriptors()
	out := make([]voice.EngineDescriptor, 0, len(descs))
	for _, want := range []string{voiceEngineLocal, voiceEngineVolcano} {
		for _, d := range descs {
			if d.ID == want {
				out = append(out, d)
			}
		}
	}
	for _, d := range descs {
		if d.ID != voiceEngineLocal && d.ID != voiceEngineVolcano {
			out = append(out, d)
		}
	}
	return out
}

func (m *voiceSettingsModel) sections() []voiceSection {
	engine := voiceSection{title: "Engine"}
	for i := range enginePickerDescriptors() {
		engine.rows = append(engine.rows, voiceRow{kind: vrowEngineOption, engineIdx: i})
	}
	if d, ok := voice.EngineDescriptorByID(m.cfg.Engine); !ok {
		engine.rows = append(engine.rows, voiceRow{kind: vrowEngineOption, engineIdx: -1})
	} else {
		for _, p := range d.Params {
			engine.rows = append(engine.rows, voiceRow{kind: vrowParam, param: p})
		}
	}
	sections := []voiceSection{engine}

	if m.cfg.Engine == voiceEngineLocal {
		models := voiceSection{title: "Models"}
		models.rows = append(models.rows, voiceRow{kind: vrowHelper})
		for i := range voice.ModelCatalog() {
			models.rows = append(models.rows, voiceRow{kind: vrowModel, modelIdx: i})
		}
		models.rows = append(models.rows, voiceRow{kind: vrowCustomPath})
		if m.precisionAvailable() {
			models.rows = append(models.rows, voiceRow{kind: vrowPrecision})
		}
		sections = append(sections, models)
	}

	input := voiceSection{title: "Input"}
	input.rows = append(input.rows,
		voiceRow{kind: vrowTest},
		voiceRow{kind: vrowThreshold},
		voiceRow{kind: vrowSilence},
		voiceRow{kind: vrowSentenceEnd},
	)
	sections = append(sections, input)

	if m.cfg.Engine == voiceEngineVolcano {
		sections = append(sections, voiceSection{
			title: "Extra features",
			rows:  []voiceRow{{kind: vrowContext}, {kind: vrowDDC}},
		})
	}
	return sections
}

func (m *voiceSettingsModel) rows() []voiceRow {
	var out []voiceRow
	for _, s := range m.sections() {
		out = append(out, s.rows...)
	}
	return out
}

func (m *voiceSettingsModel) precisionAvailable() bool {
	if m.cfg.Engine != voiceEngineLocal {
		return false
	}
	if m.cfg.CustomModelDir != "" {
		return voice.HasBothPrecisions(m.cfg.CustomModelDir)
	}
	return voice.ModelByID(m.cfg.ModelID).Kind == voice.ModelKindSenseVoice
}

func (m *voiceSettingsModel) save() tea.Cmd {
	database := m.db
	mk := m.mk
	cfg := cloneVoiceSettings(m.cfg)
	keepEngine := m.saved.Engine == cfg.Engine &&
		m.saved.DDC == cfg.DDC &&
		reflect.DeepEqual(m.saved.engineParams(cfg.Engine), cfg.engineParams(cfg.Engine))
	return func() tea.Msg {
		if err := persistVoiceSettings(database, mk, cfg); err != nil {
			return types.ErrorMsg{Err: err}
		}
		return voiceSettingsChangedMsg{cfg: cfg, keepEngine: keepEngine}
	}
}

func (m *voiceSettingsModel) saveDone(cfg voiceSettings) {
	m.saved = cloneVoiceSettings(cfg)
	m.modified = false
}

func (m *voiceSettingsModel) reset() {
	m.cfg = cloneVoiceSettings(m.saved)
	m.modified = false
	m.customErr = ""
}

func (m *voiceSettingsModel) adjust(dir int) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return
	}
	switch rows[m.cursor].kind {
	case vrowThreshold:
		m.cfg.VADThreshold = math.Round((m.cfg.VADThreshold+float64(dir)*0.05)*100) / 100
		if m.cfg.VADThreshold < 0 {
			m.cfg.VADThreshold = 0
		}
		if m.cfg.VADThreshold > 1 {
			m.cfg.VADThreshold = 1
		}
	case vrowSilence:
		m.cfg.VADSilenceMs += dir * 50
		if m.cfg.VADSilenceMs < 50 {
			m.cfg.VADSilenceMs = 50
		}
		if m.cfg.VADSilenceMs > 5000 {
			m.cfg.VADSilenceMs = 5000
		}
	case vrowSentenceEnd:
		if m.cfg.SentenceEnd == voice.SentenceEndEnter {
			m.cfg.SentenceEnd = voice.SentenceEndSpace
		} else {
			m.cfg.SentenceEnd = voice.SentenceEndEnter
		}
	case vrowContext:
		m.cfg.Context = !m.cfg.Context
	case vrowDDC:
		m.cfg.DDC = !m.cfg.DDC
	case vrowPrecision:
		m.cfg.ModelInt8 = !m.cfg.ModelInt8
		m.cfg.Verified = false
		m.testText = ""
		m.testErr = ""
	case vrowParam:
		opts := rows[m.cursor].param.Options
		if len(opts) == 0 {
			return
		}
		key := rows[m.cursor].param.Key
		cur := m.cfg.engineParams(m.cfg.Engine)[key]
		idx := -1
		for i, o := range opts {
			if o == cur {
				idx = i
			}
		}
		if idx < 0 {
			if dir > 0 {
				idx = 0
			} else {
				idx = len(opts) - 1
			}
		} else {
			idx = (idx + dir + len(opts)) % len(opts)
		}
		m.cfg.setEngineParam(m.cfg.Engine, key, opts[idx])
	default:
		return
	}
	m.modified = true
}

func (m *voiceSettingsModel) selectEngine(idx int) {
	descs := enginePickerDescriptors()
	if idx < 0 || idx >= len(descs) {
		return
	}
	if descs[idx].ID == m.cfg.Engine {
		return
	}
	m.cfg.Engine = descs[idx].ID
	m.cfg.Verified = false
	m.modified = true
}

func (m *voiceSettingsModel) startDownload(target string) tea.Cmd {
	if m.dlTarget != "" {
		return nil
	}
	m.dlErr = ""
	m.dlErrTarget = ""
	return func() tea.Msg { return voiceDownloadRequestMsg{target: target} }
}

func (m *voiceSettingsModel) helperAction() tea.Cmd {
	if !m.helperOK || m.updateAvailable() {
		return m.startDownload(voiceHelperTarget)
	}
	if m.checkingUpdate {
		return nil
	}
	m.checkingUpdate = true
	m.updateErr = ""
	return func() tea.Msg { return voiceHelperUpdateCheckRequestMsg{} }
}

func (m *voiceSettingsModel) updateAvailable() bool {
	return m.helperOK && m.updateTag != "" && m.updateTag != m.helperVersion
}

func (m *voiceSettingsModel) updateCheckDone(tag string, err error) {
	m.checkingUpdate = false
	m.updateChecked = true
	if err != nil {
		m.updateErr = err.Error()
		return
	}
	m.updateTag = tag
}

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
		m.modified = true
		return nil
	}
	return m.startDownload(spec.ID)
}

func (m *voiceSettingsModel) activate(allowEdit bool) tea.Cmd {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	r := rows[m.cursor]
	switch r.kind {
	case vrowEngineOption:
		m.selectEngine(r.engineIdx)
	case vrowHelper:
		return m.helperAction()
	case vrowModel:
		return m.modelAction(r.modelIdx)
	case vrowTest:
		if m.testing {
			return func() tea.Msg { return voiceTestRequestMsg{stop: true} }
		}
		m.testText = ""
		m.testErr = ""
		return func() tea.Msg { return voiceTestRequestMsg{} }
	case vrowCustomPath:
		if allowEdit {
			return m.startEdit()
		}
	case vrowParam:
		if len(r.param.Options) > 0 {
			m.adjust(1)
			return nil
		}
		if allowEdit {
			return m.startEdit()
		}
	default:
		m.adjust(1)
	}
	return nil
}

func (m *voiceSettingsModel) startEdit() tea.Cmd {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	r := rows[m.cursor]
	m.edit = m.cursor
	m.input.EchoMode = textinput.EchoNormal
	var v string
	switch r.kind {
	case vrowParam:
		if r.param.Secret {
			m.input.EchoMode = textinput.EchoPassword
			m.input.EchoCharacter = '*'
		}
		v = m.cfg.engineParams(m.cfg.Engine)[r.param.Key]
	case vrowCustomPath:
		m.customErr = ""
		v = m.cfg.CustomModelDir
	}
	m.input.SetWidth(max(20, m.width-30-lipgloss.Width(m.input.Prompt)))
	m.input.SetValue(v)
	return m.input.Focus()
}

func (m *voiceSettingsModel) commitEdit() {
	rows := m.rows()
	if m.edit < 0 || m.edit >= len(rows) {
		m.edit = -1
		return
	}
	r := rows[m.edit]
	v := strings.TrimSpace(m.input.Value())
	m.edit = -1
	m.input.Blur()
	switch r.kind {
	case vrowParam:
		m.cfg.setEngineParam(m.cfg.Engine, r.param.Key, v)
		m.modified = true
	case vrowCustomPath:
		if v == "" {
			m.cfg.CustomModelDir = ""
			m.customErr = ""
			m.cfg.Verified = false
			m.modified = true
			return
		}
		if !voice.ValidCustomModelDir(v) {
			m.customErr = "needs tokens.txt and model.onnx or model.int8.onnx"
			return
		}
		m.cfg.CustomModelDir = v
		m.customErr = ""
		m.cfg.Verified = false
		m.testText = ""
		m.testErr = ""
		m.modified = true
	}
}

func (m *voiceSettingsModel) handleEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.edit = -1
		m.input.Blur()
		return m, nil
	case "enter":
		m.commitEdit()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *voiceSettingsModel) handleNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := m.rows()
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "left", "h":
		m.adjust(-1)
	case "right", "l":
		m.adjust(1)
	case " ":
		return m, m.activate(false)
	case "enter":
		return m, m.activate(true)
	case "ctrl+s":
		return m, m.save()
	case "ctrl+r":
		m.reset()
	case "esc", "escape":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}
	return m, nil
}

func (m *voiceSettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.MouseWheelMsg:
		if m.edit >= 0 {
			return m, nil
		}
		maxScr := len(m.buildScrollLines()) - m.visibleRows()
		if maxScr < 0 {
			maxScr = 0
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.scroll > 0 {
				m.scroll--
			}
		case tea.MouseWheelDown:
			if m.scroll < maxScr {
				m.scroll++
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		if m.edit >= 0 || msg.Button != tea.MouseLeft || msg.Y < 2 {
			return m, nil
		}
		lineIdx := m.scroll + (msg.Y - 2)
		lines := m.buildScrollLines()
		if lineIdx < 0 || lineIdx >= len(lines) {
			return m, nil
		}
		li := lines[lineIdx].logicalIdx
		if li < 0 {
			return m, nil
		}
		m.cursor = li
		rows := m.rows()
		switch rows[li].kind {
		case vrowCustomPath:
			return m, m.startEdit()
		case vrowParam:
			if len(rows[li].param.Options) == 0 {
				return m, m.startEdit()
			}
		}
		return m, nil
	case tea.PasteMsg:
		if m.edit >= 0 {
			m.input = inputpaste.TextInput(m.input, msg)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.edit >= 0 {
			return m.handleEdit(msg)
		}
		return m.handleNormal(msg)
	}
	return m, nil
}

func (m *voiceSettingsModel) downloadStarted(target string) {
	m.dlTarget = target
	m.dlPct = 0
}

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

func (m *voiceSettingsModel) helperVersionDisplay() string {
	switch m.helperVersion {
	case "", "dev", "0.1.0":
		return "unknown version"
	}
	return m.helperVersion
}

func (m *voiceSettingsModel) helperValue() string {
	switch {
	case m.dlTarget == voiceHelperTarget:
		return fmt.Sprintf("downloading %.0f%%", m.dlPct)
	case m.dlErrTarget == voiceHelperTarget && m.dlErr != "":
		return "failed: " + m.dlErr
	case !m.helperOK:
		return "not installed"
	case m.checkingUpdate:
		return "checking for updates"
	case m.updateErr != "":
		return "update check failed: " + m.updateErr
	case m.updateAvailable():
		return "update " + m.helperVersionDisplay() + " -> " + m.updateTag
	case m.updateChecked:
		return "up to date (" + m.helperVersionDisplay() + ")"
	}
	return "installed (" + m.helperVersionDisplay() + ")"
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
		if spec.Kind == voice.ModelKindSenseVoice {
			value = "installed (fp32)"
			if m.cfg.ModelInt8 {
				value = "installed (int8)"
			}
		}
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
		return "recording... speak now"
	case m.testErr != "":
		return "failed: " + shorten(m.testErr)
	case m.testText != "":
		return "heard: " + shorten(m.testText)
	case m.cfg.Verified:
		return "verified - enter to run again"
	}
	return "enter to record a sample"
}

func (m *voiceSettingsModel) statusLine() string {
	if issue := voiceSetupIssue(m.cfg, m.modelsRoot); issue != "" {
		return "setup incomplete - " + issue
	}
	return "ready - ctrl+r starts recording outside this tab"
}

func (m *voiceSettingsModel) noticeText() string {
	if !m.fromHotkey {
		return ""
	}
	if issue := voiceSetupIssue(m.cfg, m.modelsRoot); issue != "" {
		return "voice input is not set up yet: " + issue + " below"
	}
	return ""
}

func (m *voiceSettingsModel) rowText(r voiceRow) (label, value string, dim bool) {
	switch r.kind {
	case vrowEngineOption:
		if r.engineIdx < 0 {
			label = m.cfg.Engine + " (unknown)"
			value = "[active]"
			return
		}
		d := enginePickerDescriptors()[r.engineIdx]
		label = d.Label
		if d.ID == m.cfg.Engine {
			value = "[active]"
		}
		dim = value == ""
	case vrowHelper:
		label, value = "Voice Helper", m.helperValue()
		dim = !m.helperOK
	case vrowTest:
		label, value = "Microphone test", m.testValue()
	case vrowThreshold:
		label = "speech sensitivity (0-1)"
		value = fmt.Sprintf("%.2f", m.cfg.VADThreshold)
		if m.cfg.VADThreshold == 0 {
			value = "default"
		}
	case vrowSilence:
		label, value = "end-of-sentence silence", strconv.Itoa(m.cfg.VADSilenceMs)+" ms"
	case vrowSentenceEnd:
		label, value = "Sentence end", string(m.cfg.SentenceEnd)
	case vrowContext:
		label = "Context awareness"
		value = "off"
		if m.cfg.Context {
			value = "on"
		}
		dim = !m.cfg.Context
	case vrowDDC:
		label = "Semantic smoothing (DDC)"
		value = "off"
		if m.cfg.DDC {
			value = "on"
		}
		dim = !m.cfg.DDC
	case vrowParam:
		label = r.param.Label
		v := m.cfg.engineParams(m.cfg.Engine)[r.param.Key]
		switch {
		case r.param.Secret:
			value = maskVoiceKey(v)
			dim = v == ""
		case v == "":
			value = "(not set)"
			dim = true
		default:
			value = v
		}
	case vrowModel:
		label, value = voice.ModelCatalog()[r.modelIdx].Name, m.modelValue(r.modelIdx)
		dim = !m.modelOK[r.modelIdx]
	case vrowPrecision:
		label = "Precision"
		value = "fp32"
		if m.cfg.ModelInt8 {
			value = "int8"
		}
	case vrowCustomPath:
		label, value = "Custom model path", m.customValue()
		dim = m.cfg.CustomModelDir == ""
	}
	return
}

func truncateVoiceValue(s string, max int) string {
	if max <= 0 || lipgloss.Width(s) <= max {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		if candidate := string(runes) + "..."; lipgloss.Width(candidate) <= max {
			return candidate
		}
	}
	return ""
}

func (m *voiceSettingsModel) valueWidth() int {
	w := m.width - 30
	if w < 20 {
		w = 20
	}
	return w
}

type voiceScrollLine struct {
	text       string
	logicalIdx int
}

func (m *voiceSettingsModel) rowLine(r voiceRow, idx int) string {
	cursor := "  "
	selected := idx == m.cursor
	if selected {
		cursor = "> "
	}
	label, value, dim := m.rowText(r)
	label = truncateVoiceValue(label, 24)
	if m.edit == idx {
		value = m.input.View()
	} else {
		value = truncateVoiceValue(value, m.valueWidth())
	}
	if selected {
		return fmt.Sprintf("%s%s  %s", cursor, voiceSelectedStyle.Render(voiceLabelStyle.Render(label)), voiceSelectedStyle.Render(value))
	}
	valueStyle := voiceKeyStyle
	if dim {
		valueStyle = voiceDimStyle
	}
	if strings.HasPrefix(value, "failed:") || strings.HasPrefix(value, "invalid:") {
		valueStyle = voiceErrStyle
	}
	return fmt.Sprintf("%s%s  %s", cursor, voiceLabelStyle.Render(label), valueStyle.Render(value))
}

func (m *voiceSettingsModel) buildScrollLines() []voiceScrollLine {
	var out []voiceScrollLine
	idx := 0
	for si, sec := range m.sections() {
		if si > 0 {
			out = append(out, voiceScrollLine{"", -1})
		}
		out = append(out, voiceScrollLine{voiceCatStyle.Render("  " + sec.title), -1})
		for _, r := range sec.rows {
			out = append(out, voiceScrollLine{m.rowLine(r, idx), idx})
			idx++
		}
	}
	return out
}

func (m *voiceSettingsModel) visibleRows() int {
	rows := m.height - 4
	if rows < 5 {
		rows = 5
	}
	return rows
}

func (m *voiceSettingsModel) View() tea.View {
	if rows := m.rows(); m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}

	var b strings.Builder

	title := ui.TitleStyle.Render("Voice Input")
	hints := voiceHintStyle.Render("space/enter:select/edit  left/right:change  C-s:save  C-r:reset  wheel:scroll  esc:close")
	b.WriteString(title + "  " + hints + "\n")

	if m.edit >= 0 {
		label := "value"
		rows := m.rows()
		if m.edit < len(rows) {
			label, _, _ = m.rowText(rows[m.edit])
		}
		b.WriteString(voiceCaptureStyle.Render("  Enter "+label+"...  (enter to accept, esc to cancel)") + "\n")
	} else if notice := m.noticeText(); notice != "" {
		b.WriteString(voiceCaptureStyle.Render("  "+notice) + "\n")
	} else {
		b.WriteString("\n")
	}

	lines := m.buildScrollLines()

	cursorLine := 0
	for li, sl := range lines {
		if sl.logicalIdx == m.cursor {
			cursorLine = li
			break
		}
	}

	vis := m.visibleRows()
	if m.scroll > cursorLine {
		m.scroll = cursorLine
	}
	if cursorLine >= m.scroll+vis {
		m.scroll = cursorLine - vis + 1
	}
	end := m.scroll + vis
	if end > len(lines) {
		end = len(lines)
	}

	for i := m.scroll; i < end; i++ {
		b.WriteString(lines[i].text + "\n")
	}

	footer := voiceDimStyle.Render("  " + m.statusLine())
	if m.modified {
		footer += "  " + voiceCaptureStyle.Render("* unsaved changes")
	}
	b.WriteString(footer)

	return tea.NewView(b.String())
}
