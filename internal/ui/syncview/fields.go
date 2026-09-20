package syncview

import tea "charm.land/bubbletea/v2"

type syncSection struct {
	title  string
	note   string
	fields []int
}

func (m *Model) sections() []syncSection {
	conn := []int{fieldEnabled, fieldMode}
	if m.modeIdx == 1 {
		conn = append(conn, fieldSSHHost, fieldRemotePort)
	} else {
		conn = append(conn, fieldServerURL, fieldInsecureTLS)
	}
	conn = append(conn, fieldAPIKey)
	return []syncSection{
		{title: "Connection", fields: conn},
		{title: "Encryption", note: "sync data encryption passphrase", fields: []int{fieldPassphrase}},
		{title: "Behavior", fields: []int{fieldInterval}},
	}
}

func (m *Model) visibleFields() []int {
	var fields []int
	for _, s := range m.sections() {
		fields = append(fields, s.fields...)
	}
	return fields
}

func isSecretField(field int) bool {
	return field == fieldAPIKey || field == fieldPassphrase
}

func (m *Model) effectiveAPIKey() string {
	if m.apiKeyDirty {
		return m.pendAPIKey
	}
	return m.loadedAPIKey
}

func (m *Model) effectivePass() string {
	if m.passDirty {
		return m.pendPass
	}
	return m.loadedPass
}

func (m *Model) currentField() int {
	vf := m.visibleFields()
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
	return vf[m.focused]
}

func (m *Model) isSelector(field int) bool {
	return field == fieldEnabled || field == fieldMode || field == fieldSSHHost || field == fieldInsecureTLS
}

func (m *Model) inputIdxForField(field int) int {
	switch field {
	case fieldRemotePort:
		return inRemotePort
	case fieldServerURL:
		return inServerURL
	case fieldAPIKey:
		return inAPIKey
	case fieldPassphrase:
		return inPassphrase
	case fieldInterval:
		return inInterval
	}
	return -1
}

func (m *Model) blurAll() {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
}

func (m *Model) focusCurrent() tea.Cmd {
	m.blurAll()
	f := m.currentField()
	if isSecretField(f) {
		return nil
	}
	idx := m.inputIdxForField(f)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}
