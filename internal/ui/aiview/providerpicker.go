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
	inputs []textinput.Model
	focus  int
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
	rows := []string{ui.TitleStyle.Render("Add Provider"), ""}
	for i, label := range providerFormLabels {
		rows = append(rows, ui.DimStyle.Render(fmt.Sprintf("%-9s", label))+f.inputs[i].View())
	}
	rows = append(rows, "",
		ui.DimStyle.Render("tab next | enter submit | esc cancel"))
	return strings.Join(rows, "\n")
}

func (m *Model) providersView() string {
	rows := []string{ui.TitleStyle.Render("Providers"), ""}
	if len(m.providers) == 0 {
		rows = append(rows, ui.DimStyle.Render("No providers configured"))
	}
	for i, p := range m.providers {
		cursor := "  "
		style := ui.DimStyle
		if i == m.pCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		active := ""
		if p.Name == m.store.Active() {
			active = ui.SuccessStyle.Render(" [active]")
		}
		detail := p.Type
		if p.Model != "" {
			detail += " · " + p.Model
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s",
			cursor, style.Render(p.Name), ui.DimStyle.Render("["+detail+"]"), active))
	}
	rows = append(rows, "",
		ui.DimStyle.Render("enter switch | a add | esc back"))
	return strings.Join(rows, "\n")
}

func (m *Model) updateProviders(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeChat
	case "up", "k":
		if m.pCursor > 0 {
			m.pCursor--
		}
	case "down", "j":
		if m.pCursor < len(m.providers)-1 {
			m.pCursor++
		}
	case "enter":
		if m.pCursor < len(m.providers) {
			m.store.Switch(m.providers[m.pCursor].Name)
		}
	case "a":
		m.mode = modeProviderForm
		m.form = newProviderForm(m.contentWidth() - 12)
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
			m.store.Add(p)
			m.providers = m.store.Providers()
			m.mode = modeProviders
		}
		return nil
	}
	return cmd
}
