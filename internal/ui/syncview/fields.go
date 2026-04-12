package syncview

import tea "charm.land/bubbletea/v2"

func (m *Model) visibleFields() []int {
	fields := []int{fieldEnabled, fieldMode}
	if m.modeIdx == 0 { // SSH
		fields = append(fields, fieldSSHHost, fieldRemoteBin, fieldRemoteDB)
	} else { // HTTP/HTTPS
		fields = append(fields, fieldServerURL, fieldAPIKey)
	}
	fields = append(fields, fieldPassphrase, fieldInterval)
	return fields
}

func (m *Model) currentField() int {
	vf := m.visibleFields()
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
	return vf[m.focused]
}

func (m *Model) isSelector(field int) bool {
	return field == fieldEnabled || field == fieldMode || field == fieldSSHHost
}

func (m *Model) inputIdxForField(field int) int {
	switch field {
	case fieldRemoteBin:
		return inRemoteBin
	case fieldRemoteDB:
		return inRemoteDB
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
	idx := m.inputIdxForField(f)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}
