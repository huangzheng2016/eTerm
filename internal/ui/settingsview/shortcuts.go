package settingsview

import (
	"encoding/json"

	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

type ShortcutsModel struct {
	db           *gorm.DB
	entries      []bindingEntry
	extra        map[string]json.RawMessage
	cursor       int
	state        editState
	width        int
	height       int
	scroll       int
	modified     bool
	defaultsJSON []byte

	confirmReset components.ConfirmModel
}

func NewShortcuts(database *gorm.DB, configJSON []byte, defaultsJSON []byte) *ShortcutsModel {
	m := &ShortcutsModel{
		db:           database,
		defaultsJSON: defaultsJSON,
		confirmReset: components.NewConfirm("Reset shortcuts", "Restore all key bindings to factory defaults?"),
	}
	m.entries = buildEntries(configJSON)
	m.extra = map[string]json.RawMessage{}
	_ = json.Unmarshal(configJSON, &m.extra)
	return m
}

func (m *ShortcutsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *ShortcutsModel) ConfigJSON() []byte {
	merged := make(map[string]json.RawMessage, len(m.extra)+len(m.entries))
	for k, v := range m.extra {
		merged[k] = v
	}
	for _, e := range m.entries {
		data, _ := json.Marshal(e.Keys)
		merged[e.Field] = data
	}
	data, _ := json.Marshal(merged)
	return data
}

func (m *ShortcutsModel) groupStarts() []int {
	var starts []int
	lastCat := ""
	for i, e := range m.entries {
		if e.Category != lastCat {
			lastCat = e.Category
			starts = append(starts, i)
		}
	}
	return starts
}

func (m *ShortcutsModel) currentGroup(starts []int) int {
	g := 0
	for i, s := range starts {
		if s > m.cursor {
			break
		}
		g = i
	}
	return g
}
