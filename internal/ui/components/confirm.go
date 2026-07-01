package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type ConfirmModel struct {
	title       string
	message     string
	confirmed   bool
	active      bool
	yesSelected bool
}

const (
	confirmYesButton = "  Yes  "
	confirmNoButton  = "  No   "
)

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
			if hit, yes := c.mouseButton(msg.X, msg.Y); hit {
				c.confirmed = yes
				c.active = false
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

	yesBtn := confirmYesButton
	noBtn := confirmNoButton
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

func (c ConfirmModel) mouseButton(x, y int) (bool, bool) {
	buttonY := 6 + strings.Count(c.message, "\n")
	if y != buttonY {
		return false, false
	}
	yesStart := 3
	yesEnd := yesStart + len(confirmYesButton)
	noStart := yesEnd + 4
	noEnd := noStart + len(confirmNoButton)
	switch {
	case x >= yesStart && x < yesEnd:
		return true, true
	case x >= noStart && x < noEnd:
		return true, false
	default:
		return false, false
	}
}
