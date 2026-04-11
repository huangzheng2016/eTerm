package components

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/eterm/eterm/internal/ui"
)

type ConfirmModel struct {
	title       string
	message     string
	confirmed   bool
	active      bool
	yesSelected bool
}

func NewConfirm(title, message string) ConfirmModel {
	return ConfirmModel{
		title:   title,
		message: message,
	}
}

func (c ConfirmModel) IsActive() bool {
	return c.active
}

func (c ConfirmModel) Show() ConfirmModel {
	c.active = true
	c.confirmed = false
	c.yesSelected = false
	return c
}

func (c ConfirmModel) Result() bool {
	return c.confirmed
}

func (c ConfirmModel) Update(msg tea.Msg) (ConfirmModel, tea.Cmd) {
	if !c.active {
		return c, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "Y":
			c.confirmed = true
			c.active = false
		case "n", "N", "esc":
			c.confirmed = false
			c.active = false
		case "enter":
			c.confirmed = c.yesSelected
			c.active = false
		case "left", "h":
			c.yesSelected = true
		case "right", "l":
			c.yesSelected = false
		case "tab":
			c.yesSelected = !c.yesSelected
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if msg.Y >= 4 && msg.Y <= 4 {
				if msg.X >= 2 && msg.X < 10 {
					c.confirmed = true
					c.active = false
				} else if msg.X >= 12 && msg.X < 20 {
					c.confirmed = false
					c.active = false
				}
			}
		}
	}

	return c, nil
}

func (c ConfirmModel) View() string {
	if !c.active {
		return ""
	}

	titleStr := ui.TitleStyle.Render(c.title)
	msgStr := c.message

	yesBtn := "  Yes  "
	noBtn := "  No   "
	if c.yesSelected {
		yesBtn = ui.SelectedStyle.Render(yesBtn)
		noBtn = ui.DimStyle.Render(noBtn)
	} else {
		yesBtn = ui.DimStyle.Render(yesBtn)
		noBtn = ui.SelectedStyle.Render(noBtn)
	}

	buttons := fmt.Sprintf("%s    %s", yesBtn, noBtn)

	content := lipgloss.JoinVertical(lipgloss.Left, titleStr, "", msgStr, "", buttons)

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)

	return dialog
}
