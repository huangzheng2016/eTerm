package app

import (
	"fmt"
	"math"
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
	voiceRowThreshold
	voiceRowSentenceEnd
	voiceRowAPIKey
	voiceRowAppKey
	voiceRowAccessKey
	voiceRowCount
)

// voiceSettingsModel is the voice input settings overlay (opened from the
// command palette; settingsview's keybinding section is a separate surface).
type voiceSettingsModel struct {
	db     *gorm.DB
	mk     *security.MasterKeyManager
	cfg    voiceSettings
	cursor int
	edit   int // editing key row, -1 when not editing
	input  textinput.Model
}

func newVoiceSettingsModel(database *gorm.DB, mk *security.MasterKeyManager, cfg voiceSettings) *voiceSettingsModel {
	ti := textinput.New()
	ti.CharLimit = 256
	return &voiceSettingsModel{db: database, mk: mk, cfg: cfg, edit: -1, input: ti}
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

// adjust cycles the enum rows and steps the threshold; returns the persist
// command when the row changed.
func (m *voiceSettingsModel) adjust(dir int) tea.Cmd {
	switch m.cursor {
	case voiceRowEngine:
		if m.cfg.Engine == voiceEngineLocal {
			m.cfg.Engine = voiceEngineVolcano
		} else {
			m.cfg.Engine = voiceEngineLocal
		}
	case voiceRowThreshold:
		m.cfg.VADThreshold = math.Round((m.cfg.VADThreshold+float64(dir)*0.05)*100) / 100
		if m.cfg.VADThreshold < 0 {
			m.cfg.VADThreshold = 0
		}
		if m.cfg.VADThreshold > 1 {
			m.cfg.VADThreshold = 1
		}
		return m.persist(true)
	case voiceRowSentenceEnd:
		if m.cfg.SentenceEnd == voice.SentenceEndEnter {
			m.cfg.SentenceEnd = voice.SentenceEndSpace
		} else {
			m.cfg.SentenceEnd = voice.SentenceEndEnter
		}
		return m.persist(true)
	default:
		return nil
	}
	return m.persist(false)
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
		if m.cursor >= voiceRowAPIKey {
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

func maskVoiceKey(v string) string {
	if v == "" {
		return "(not set)"
	}
	return "(set)"
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
		{"VAD threshold", threshold},
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
		lines = append(lines, fmt.Sprintf("%s%s %s", cursor, style.Render(fmt.Sprintf("%-18s", r.label)), ui.DimStyle.Render(value)))
	}
	lines = append(lines, "", ui.DimStyle.Render("up/down move · left/right change · enter edit · esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}
