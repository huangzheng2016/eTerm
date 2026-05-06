package settingsview

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
)

type passwordOverlay struct {
	noPassword bool
	current    textinput.Model
	newPass    textinput.Model
	confirm    textinput.Model
	focus      int
	err        string
	width      int
	height     int
}

func newPasswordOverlay(noPassword bool, width, height int) *passwordOverlay {
	cur := textinput.New()
	cur.Placeholder = "Current password"
	cur.EchoMode = textinput.EchoPassword
	cur.EchoCharacter = '*'

	np := textinput.New()
	np.Placeholder = "New password"
	np.EchoMode = textinput.EchoPassword
	np.EchoCharacter = '*'

	cf := textinput.New()
	cf.Placeholder = "Confirm new password"
	cf.EchoMode = textinput.EchoPassword
	cf.EchoCharacter = '*'

	o := &passwordOverlay{
		noPassword: noPassword,
		current:    cur,
		newPass:    np,
		confirm:    cf,
	}
	if noPassword {
		o.focus = 1
		_ = o.newPass.Focus()
	} else {
		o.focus = 0
		_ = o.current.Focus()
	}
	o.SetSize(width, height)
	return o
}

func (o *passwordOverlay) SetSize(width, height int) {
	o.width = width
	o.height = height
	iw := 40
	if width > 0 {
		iw = min(40, max(24, width-12))
	}
	o.current.SetWidth(iw)
	o.newPass.SetWidth(iw)
	o.confirm.SetWidth(iw)
}

func (o *passwordOverlay) Init() tea.Cmd {
	return textinput.Blink
}

func (o *passwordOverlay) blurAll() {
	o.current.Blur()
	o.newPass.Blur()
	o.confirm.Blur()
}

func (o *passwordOverlay) focusNext() tea.Cmd {
	if o.noPassword {
		if o.focus == 1 {
			o.focus = 2
			o.blurAll()
			return o.confirm.Focus()
		}
		o.focus = 1
		o.blurAll()
		return o.newPass.Focus()
	}
	switch o.focus {
	case 0:
		o.focus = 1
		o.blurAll()
		return o.newPass.Focus()
	case 1:
		o.focus = 2
		o.blurAll()
		return o.confirm.Focus()
	default:
		o.focus = 0
		o.blurAll()
		return o.current.Focus()
	}
}

func (o *passwordOverlay) activeInput() *textinput.Model {
	switch o.focus {
	case 0:
		return &o.current
	case 1:
		return &o.newPass
	default:
		return &o.confirm
	}
}

func (o *passwordOverlay) submitCmd() tea.Cmd {
	o.err = ""
	cur := o.current.Value()
	nw := o.newPass.Value()
	cf := o.confirm.Value()
	if o.noPassword {
		cur = ""
	}
	if !o.noPassword && cur == "" {
		o.err = "Enter current password"
		return nil
	}
	if nw == "" {
		o.err = "New password cannot be empty"
		return nil
	}
	if nw != cf {
		o.err = "New passwords do not match"
		return nil
	}
	return func() tea.Msg {
		return types.MasterPasswordChangeMsg{Current: cur, New: nw}
	}
}

func (o *passwordOverlay) Update(msg tea.Msg) (*passwordOverlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		o.SetSize(msg.Width, msg.Height)
		return o, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return nil, nil
		case "tab", "shift+tab":
			return o, o.focusNext()
		case "enter":
			if o.noPassword {
				if o.focus == 1 {
					return o, o.focusNext()
				}
				if cmd := o.submitCmd(); cmd != nil {
					return nil, cmd
				}
				return o, nil
			}
			if o.focus < 2 {
				return o, o.focusNext()
			}
			if cmd := o.submitCmd(); cmd != nil {
				return nil, cmd
			}
			return o, nil
		}
	}

	in := o.activeInput()
	if in == nil {
		return o, nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return o, cmd
}

func (o *passwordOverlay) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Master password")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("tab: fields  enter: next/submit  esc: cancel")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(hint)
	b.WriteString("\n\n")
	if !o.noPassword {
		b.WriteString(o.current.View())
		b.WriteString("\n\n")
	}
	b.WriteString(o.newPass.View())
	b.WriteString("\n\n")
	b.WriteString(o.confirm.View())
	if o.err != "" {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render(o.err))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(b.String())

	h := o.height
	if h <= 0 {
		h = lipgloss.Height(box) + 2
	}
	w := o.width
	if w <= 0 {
		return box
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
