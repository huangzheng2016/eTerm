package fwdview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestEnterTogglesForward(t *testing.T) {
	m := Model{
		rules:   []db.PortForward{{Model: gorm.Model{ID: 1}}},
		running: map[uint]bool{},
		vk:      viewkeys.FwdKeys{Start: []string{"enter"}},
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if msg := cmd(); msg != (types.ForwardRuleStartMsg{RuleID: 1}) {
		t.Fatalf("stopped rule command = %#v", msg)
	}

	m.running[1] = true
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if msg := cmd(); msg != (types.ForwardRuleStopMsg{RuleID: 1}) {
		t.Fatalf("running rule command = %#v", msg)
	}
}

func TestDoubleClickTogglesForward(t *testing.T) {
	m := New(nil, viewkeys.FwdKeys{})
	m.rules = []db.PortForward{{Model: gorm.Model{ID: 1}}}
	m.SetSize(80, 20)
	click := tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft})

	updated, cmd := m.Update(click)
	if cmd != nil {
		t.Fatal("single click toggled forward")
	}
	_, cmd = updated.(Model).Update(click)
	if msg := cmd(); msg != (types.ForwardRuleStartMsg{RuleID: 1}) {
		t.Fatalf("double click command = %#v", msg)
	}
}
