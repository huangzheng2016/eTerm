package fwdeditor

import (
	"strconv"

	"charm.land/bubbles/v2/textinput"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
)

const (
	hostField      = 0
	directionField = 1
	localPortField = 2
	remoteHostField = 3
	remotePortField = 4
)

const inputCount = 3 // localPort, remoteHost, remotePort

var directionOptions = []string{"local", "remote", "dynamic"}
var directionDisplay = []string{"Local (-L)", "Remote (-R)", "Dynamic (-D)"}

var fieldLabels = map[int]string{
	hostField:       "Host",
	directionField:  "Direction",
	localPortField:  "Local port",
	remoteHostField: "Remote host",
	remotePortField: "Remote port",
}

type Model struct {
	inputs       [inputCount]textinput.Model
	focused      int
	db           *gorm.DB
	width        int
	height       int
	err          string
	ruleID       uint // 0 = new, >0 = editing
	hostOptions  []db.Host
	hostIdx      int
	directionIdx int
}

// PLACEHOLDER_MORE

func (m Model) visibleFields() []int {
	fields := []int{hostField, directionField, localPortField}
	if directionOptions[m.directionIdx] != "dynamic" {
		fields = append(fields, remoteHostField, remotePortField)
	}
	return fields
}

func inputIndexForField(field int) int {
	switch field {
	case localPortField:
		return 0
	case remoteHostField:
		return 1
	case remotePortField:
		return 2
	}
	return -1
}

func (m Model) currentField() int {
	vf := m.visibleFields()
	if m.focused < 0 || m.focused >= len(vf) {
		return -1
	}
	return vf[m.focused]
}

func New(database *gorm.DB, rule *db.PortForward) Model {
	var inputs [inputCount]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[0].Placeholder = "8080"
	inputs[1].Placeholder = "localhost"
	inputs[2].Placeholder = "80"

	m := Model{
		inputs: inputs,
		db:     database,
	}
	m.syncInputWidths()

	if rule != nil && rule.ID > 0 {
		m.ruleID = rule.ID
		inputs[0].SetValue(strconv.Itoa(rule.LocalPort))
		inputs[1].SetValue(rule.RemoteHost)
		inputs[2].SetValue(strconv.Itoa(rule.RemotePort))
		for i, d := range directionOptions {
			if d == rule.Direction {
				m.directionIdx = i
				break
			}
		}
	}

	return m
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.syncInputWidths()
}

func (m *Model) syncInputWidths() {
	// formStyle has Width(50) and Padding(1,3), so inner content is ~42 chars.
	// Label is 14 chars wide. Input gets the rest.
	iw := 24
	if m.width > 0 && m.width < 50 {
		iw = max(10, m.width-26)
	}
	for i := range m.inputs {
		m.inputs[i].SetWidth(iw)
	}
}

func hostLabel(h db.Host) string {
	if h.Alias != "" {
		return h.Alias
	}
	return h.Hostname
}
