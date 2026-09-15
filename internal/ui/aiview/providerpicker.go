package aiview

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
)

var providerFormLabels = []string{"name", "type", "base_url", "api_key", "model"}

type providerForm struct {
	inputs  []textinput.Model
	focus   int
	editing string
}

func newProviderForm(width int) providerForm {
	f := providerForm{inputs: make([]textinput.Model, len(providerFormLabels))}
	for i, label := range providerFormLabels {
		in := textinput.New()
		in.Placeholder = label
		in.SetWidth(width)
		if label == "api_key" {
			in.EchoMode = textinput.EchoPassword
		}
		f.inputs[i] = in
	}
	f.inputs[0].Focus()
	return f
}

func newProviderEditForm(width int, e ModelEntry) providerForm {
	f := newProviderForm(width)
	f.editing = e.Provider
	f.inputs[0].SetValue(e.Provider)
	f.inputs[1].SetValue(e.Type)
	f.inputs[2].SetValue(e.BaseURL)
	f.inputs[3].Placeholder = "api_key (empty keeps current)"
	f.inputs[4].SetValue(e.DefaultModel)
	return f
}

func (f *providerForm) update(msg tea.KeyPressMsg) (submitted, cancelled bool, cmd tea.Cmd) {
	switch msg.String() {
	case "esc":
		return false, true, nil
	case "tab", "down":
		f.focusInput((f.focus + 1) % len(f.inputs))
		return false, false, nil
	case "shift+tab", "up":
		f.focusInput((f.focus - 1 + len(f.inputs)) % len(f.inputs))
		return false, false, nil
	case "enter":
		if f.focus == len(f.inputs)-1 {
			return true, false, nil
		}
		f.focusInput(f.focus + 1)
		return false, false, nil
	}
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return false, false, cmd
}

func (f *providerForm) focusInput(i int) {
	f.inputs[f.focus].Blur()
	f.focus = i
	f.inputs[f.focus].Focus()
}

func (f *providerForm) provider() Provider {
	v := func(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
	return Provider{
		Name:    v(0),
		Type:    v(1),
		BaseURL: v(2),
		APIKey:  f.inputs[3].Value(),
		Model:   v(4),
	}
}

func (f *providerForm) view() string {
	title := "Add Provider"
	if f.editing != "" {
		title = "Edit Provider"
	}
	rows := []string{ui.TitleStyle.Render(title), ""}
	for i, label := range providerFormLabels {
		rows = append(rows, ui.DimStyle.Render(fmt.Sprintf("%-9s", label))+f.inputs[i].View())
	}
	rows = append(rows, "",
		ui.DimStyle.Render("tab next | enter submit | esc cancel"))
	return strings.Join(rows, "\n")
}

func (m *Model) providersView() string {
	rows := []string{ui.TitleStyle.Render("Models"), ""}
	if len(m.models) == 0 {
		rows = append(rows, ui.DimStyle.Render("No models configured"))
	}
	for i, e := range m.models {
		cursor := "  "
		style := ui.DimStyle
		if i == m.pCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		active := ""
		if e.Label == m.store.Active() {
			active = ui.SuccessStyle.Render(" [active]")
		}
		detail := e.Type
		if e.Provider != e.Label {
			detail += " · " + e.Provider
		}
		if e.KeySet {
			detail += " (set)"
		}
		if e.ReadOnly {
			detail += " [kimi]"
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s",
			cursor, style.Render(e.Label), ui.DimStyle.Render("["+detail+"]"), active))
	}
	rows = append(rows, "")
	if m.delConfirm != "" {
		rows = append(rows, ui.DimStyle.Render(fmt.Sprintf("delete %s? (y/n)", m.delConfirm)))
	} else {
		rows = append(rows, ui.DimStyle.Render("enter select | a add | e edit | d delete | esc back"))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) updateProviders(msg tea.KeyPressMsg) tea.Cmd {
	if m.delConfirm != "" {
		switch msg.String() {
		case "y", "enter":
			_ = m.store.Delete(m.delConfirm)
			m.delConfirm = ""
			m.models = m.store.Models()
			if m.pCursor >= len(m.models) {
				m.pCursor = len(m.models) - 1
			}
			if m.pCursor < 0 {
				m.pCursor = 0
			}
		case "n", "esc":
			m.delConfirm = ""
		}
		return nil
	}
	switch msg.String() {
	case "esc":
		m.mode = modeChat
	case "up", "k":
		if m.pCursor > 0 {
			m.pCursor--
		}
	case "down", "j":
		if m.pCursor < len(m.models)-1 {
			m.pCursor++
		}
	case "enter":
		if m.pCursor < len(m.models) {
			e := m.models[m.pCursor]
			m.store.Switch(e.Provider, e.Model)
		}
	case "a":
		m.mode = modeProviderForm
		m.form = newProviderForm(m.contentWidth() - 12)
	case "e":
		if m.pCursor < len(m.models) {
			e := m.models[m.pCursor]
			if !e.ReadOnly {
				m.mode = modeProviderForm
				m.form = newProviderEditForm(m.contentWidth()-12, e)
			}
		}
	case "d":
		if m.pCursor < len(m.models) {
			e := m.models[m.pCursor]
			if !e.ReadOnly {
				m.delConfirm = e.Provider
			}
		}
	}
	return nil
}

func (m *Model) updateProviderForm(msg tea.KeyPressMsg) tea.Cmd {
	submitted, cancelled, cmd := m.form.update(msg)
	if cancelled {
		m.mode = modeProviders
		return nil
	}
	if submitted {
		p := m.form.provider()
		if p.Name != "" {
			if m.form.editing != "" {
				_ = m.store.Update(m.form.editing, p)
			} else {
				m.store.Add(p)
			}
			m.models = m.store.Models()
			m.mode = modeProviders
		}
		return nil
	}
	return cmd
}
