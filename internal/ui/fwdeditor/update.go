package fwdeditor

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

type hostsLoadedMsg struct {
	hosts []db.Host
	err   error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadHosts(), textinput.Blink)
}

func (m Model) loadHosts() tea.Cmd {
	return func() tea.Msg {
		var hosts []db.Host
		err := m.db.Order("alias").Find(&hosts).Error
		return hostsLoadedMsg{hosts: hosts, err: err}
	}
}

// PLACEHOLDER_UPDATE

func (m *Model) blurAll() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
}

func (m *Model) focusCurrent() tea.Cmd {
	m.blurAll()
	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}

func (m *Model) clampFocus() {
	vf := m.visibleFields()
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
	if m.focused < 0 {
		m.focused = 0
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hostsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.hostOptions = msg.hosts
			// If editing, find the host index
			if m.ruleID > 0 {
				var rule db.PortForward
				if err := m.db.First(&rule, m.ruleID).Error; err == nil {
					for i, h := range m.hostOptions {
						if h.ID == rule.HostID {
							m.hostIdx = i
							break
						}
					}
				}
			}
		}
		return m, m.focusCurrent()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		vf := m.visibleFields()
		field := m.currentField()

		switch msg.String() {
		case "tab", "down":
			m.blurAll()
			m.focused = (m.focused + 1) % len(vf)
			return m, m.focusCurrent()
		case "shift+tab", "up":
			m.blurAll()
			m.focused = (m.focused - 1 + len(vf)) % len(vf)
			return m, m.focusCurrent()
		case "left":
			if field == hostField && len(m.hostOptions) > 0 {
				m.hostIdx = (m.hostIdx - 1 + len(m.hostOptions)) % len(m.hostOptions)
				return m, nil
			}
			if field == directionField {
				m.directionIdx = (m.directionIdx - 1 + len(directionOptions)) % len(directionOptions)
				m.clampFocus()
				return m, nil
			}
		case "right":
			if field == hostField && len(m.hostOptions) > 0 {
				m.hostIdx = (m.hostIdx + 1) % len(m.hostOptions)
				return m, nil
			}
			if field == directionField {
				m.directionIdx = (m.directionIdx + 1) % len(directionOptions)
				m.clampFocus()
				return m, nil
			}
		case "ctrl+s":
			return m, m.save()
		case "enter":
			if m.focused == len(vf)-1 {
				return m, m.save()
			}
			m.blurAll()
			m.focused = (m.focused + 1) % len(vf)
			return m, m.focusCurrent()
		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}

		// Forward to focused textinput
		idx := inputIndexForField(field)
		if idx >= 0 {
			var cmd tea.Cmd
			m.inputs[idx], cmd = m.inputs[idx].Update(msg)
			return m, cmd
		}

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		rendered, fieldYs, actionY := m.renderForm()
		ox, oy, ow, oh := m.centeredBounds(rendered)
		lx := msg.X - ox
		ly := msg.Y - oy
		if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
			return m, nil
		}
		if ly == actionY {
			if lx < ow/2 {
				return m, m.save()
			}
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}
		vf := m.visibleFields()
		for i, y := range fieldYs {
			if ly != y || i >= len(vf) {
				continue
			}
			m.focused = i
			field := vf[i]
			if field == hostField || field == directionField {
				dir := 1
				if lx < ow/2 {
					dir = -1
				}
				if field == hostField && len(m.hostOptions) > 0 {
					m.hostIdx = (m.hostIdx + dir + len(m.hostOptions)) % len(m.hostOptions)
					return m, nil
				}
				if field == directionField {
					m.directionIdx = (m.directionIdx + dir + len(directionOptions)) % len(directionOptions)
					m.clampFocus()
					return m, nil
				}
			}
			return m, m.focusCurrent()
		}
	}
	return m, nil
}

func (m Model) save() tea.Cmd {
	if len(m.hostOptions) == 0 {
		m.err = "No hosts available"
		return nil
	}
	hostID := m.hostOptions[m.hostIdx].ID
	direction := directionOptions[m.directionIdx]

	lpStr := strings.TrimSpace(m.inputs[0].Value())
	lp, err := strconv.Atoi(lpStr)
	if err != nil || lp <= 0 || lp > 65535 {
		m.err = "Invalid local port"
		return nil
	}

	remoteHost := "localhost"
	remotePort := 0
	if direction != "dynamic" {
		remoteHost = strings.TrimSpace(m.inputs[1].Value())
		if remoteHost == "" {
			remoteHost = "localhost"
		}
		rpStr := strings.TrimSpace(m.inputs[2].Value())
		rp, err := strconv.Atoi(rpStr)
		if err != nil || rp <= 0 || rp > 65535 {
			m.err = "Invalid remote port"
			return nil
		}
		remotePort = rp
	}

	rule := db.PortForward{
		HostID:     hostID,
		LocalPort:  lp,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
		Direction:  direction,
	}

	database := m.db
	ruleID := m.ruleID

	return func() tea.Msg {
		if ruleID > 0 {
			rule.ID = ruleID
			if err := database.Save(&rule).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
		} else {
			if err := database.Create(&rule).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
		}
		return types.ForwardRuleSavedMsg{Rule: rule}
	}
}
