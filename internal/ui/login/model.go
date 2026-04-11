package login

import (
	"encoding/base64"
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/types"
)

type Model struct {
	passwordInput textinput.Model
	confirmInput  textinput.Model
	masterKey     *security.MasterKeyManager
	isSetup       bool
	focused       int
	err           string
	width         int
	height        int
}

func New(masterKey *security.MasterKeyManager, isSetup bool) Model {
	pi := textinput.New()
	pi.Placeholder = "Master Password"
	pi.EchoMode = textinput.EchoPassword
	pi.EchoCharacter = '*'
	pi.Focus()

	ci := textinput.New()
	ci.Placeholder = "Confirm Password"
	ci.EchoMode = textinput.EchoPassword
	ci.EchoCharacter = '*'

	out := Model{
		passwordInput: pi,
		confirmInput:  ci,
		masterKey:     masterKey,
		isSetup:       isSetup,
		focused:       0,
	}
	out.syncPasswordInputWidths()
	return out
}

// syncPasswordInputWidths avoids bubbles textinput showing only the first placeholder character when Width<=0.
func (m *Model) syncPasswordInputWidths() {
	// boxStyle.Width(50) minus Padding(2,4) → inner ~42
	iw := 42
	if m.width > 0 {
		iw = min(42, max(24, m.width-16))
	}
	m.passwordInput.SetWidth(iw)
	m.confirmInput.SetWidth(iw)
}

func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	m.syncPasswordInputWidths()
	return m
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncPasswordInputWidths()
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if m.isSetup {
				if m.focused == 0 {
					m.focused = 1
					m.passwordInput.Blur()
					cmd := m.confirmInput.Focus()
					return m, cmd
				}
				pw := m.passwordInput.Value()
				cpw := m.confirmInput.Value()
				if pw != cpw {
					m.err = "Passwords do not match"
					return m, nil
				}
				if pw == "" {
					m.err = "Password cannot be empty"
					return m, nil
				}
				salt, verifier := m.masterKey.Setup([]byte(pw))
				saltB64 := base64.StdEncoding.EncodeToString(salt)
				verifierB64 := base64.StdEncoding.EncodeToString(verifier)
				return m, func() tea.Msg {
					return types.MasterKeyUnlockedMsg{
						Salt:     saltB64,
						Verifier: verifierB64,
						IsSetup:  true,
					}
				}
			}
			pw := m.passwordInput.Value()
			if pw == "" {
				m.err = "Password cannot be empty"
				return m, nil
			}
			if m.masterKey.Unlock([]byte(pw)) {
				return m, func() tea.Msg { return types.MasterKeyUnlockedMsg{} }
			}
			m.err = "Invalid password"
			return m, nil

		case "tab", "shift+tab":
			if m.isSetup {
				if m.focused == 0 {
					m.focused = 1
					m.passwordInput.Blur()
					cmd := m.confirmInput.Focus()
					return m, cmd
				}
				m.focused = 0
				m.confirmInput.Blur()
				cmd := m.passwordInput.Focus()
				return m, cmd
			}

		case "esc":
			if m.isSetup {
				salt, verifier := m.masterKey.SetupNoPassword()
				saltB64 := base64.StdEncoding.EncodeToString(salt)
				verifierB64 := base64.StdEncoding.EncodeToString(verifier)
				return m, func() tea.Msg {
					return types.MasterKeyUnlockedMsg{
						IsSetup:    true,
						Salt:       saltB64,
						Verifier:   verifierB64,
						NoPassword: true,
					}
				}
			}
			return m, tea.Quit
		}
	}

	if m.focused == 0 {
		var cmd tea.Cmd
		m.passwordInput, cmd = m.passwordInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.confirmInput, cmd = m.confirmInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() tea.View {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		MarginBottom(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666")).
		MarginBottom(1)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000")).
		Bold(true).
		MarginTop(1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(2, 4).
		Width(50)

	title := titleStyle.Render("🔐 eTerm")

	var subtitle string
	if m.isSetup {
		subtitle = subtitleStyle.Render("Set Up Master Password")
	} else {
		subtitle = subtitleStyle.Render("Enter Master Password")
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		m.passwordInput.View(),
	)

	if m.isSetup {
		content = lipgloss.JoinVertical(lipgloss.Left,
			title,
			subtitle,
			m.passwordInput.View(),
			"",
			m.confirmInput.View(),
			"",
			subtitleStyle.Render("Press Esc to skip (no password)"),
		)
	}

	if m.err != "" {
		content = lipgloss.JoinVertical(lipgloss.Left,
			content,
			errorStyle.Render(fmt.Sprintf("⚠ %s", m.err)),
		)
	}

	box := boxStyle.Render(content)

	if m.width > 0 && m.height > 0 {
		box = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	return tea.NewView(box)
}
