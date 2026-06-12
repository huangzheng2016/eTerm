package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type batchActionItem int

const (
	batchActionOpen batchActionItem = iota
	batchActionSnippet
	batchActionReadOnly
)

type batchActionsModel struct {
	cursor  batchActionItem
	hostIDs []uint
	step    int
	command textinput.Model
}

func newBatchActionsModel(ids []uint) *batchActionsModel {
	ti := textinput.New()
	ti.Placeholder = "Readonly shell command"
	return &batchActionsModel{hostIDs: append([]uint(nil), ids...), command: ti}
}

func (b *batchActionsModel) syncWidth(termW int) {
	w := termW - 16
	if w < 24 {
		w = 24
	}
	if w > 72 {
		w = 72
	}
	b.command.SetWidth(w)
}

func (b *batchActionsModel) View() string {
	title := ui.TitleStyle.Render("Batch Actions")
	sub := ui.DimStyle.Render(fmt.Sprintf("%d host(s)", len(b.hostIDs)))
	if b.step == 1 {
		content := lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			sub,
			"",
			lipgloss.NewStyle().Bold(true).Render("Read-only command"),
			b.command.View(),
			"",
			ui.DimStyle.Render("Enter run · Esc back · click left run / right back"),
		)
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2).
			Render(content)
	}
	items := []string{
		renderBatchActionRow("Open SSH Tabs", b.cursor == batchActionOpen),
		renderBatchActionRow("Broadcast Snippet", b.cursor == batchActionSnippet),
		renderBatchActionRow("Read-Only Command", b.cursor == batchActionReadOnly),
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		sub,
		"",
		lipgloss.JoinVertical(lipgloss.Left, items...),
		"",
		ui.DimStyle.Render("Enter choose · Esc cancel · click row"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

func renderBatchActionRow(label string, selected bool) string {
	if selected {
		return ui.SelectedStyle.Render("> " + label)
	}
	return ui.DimStyle.Render("  " + label)
}

func (a App) handleBatchActionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch a.batchActions.step {
	case 1:
		switch msg.String() {
		case "esc":
			a.batchActions.step = 0
			return a, nil
		case "enter":
			command := strings.TrimSpace(a.batchActions.command.Value())
			if command == "" {
				return a, nil
			}
			hostIDs := append([]uint(nil), a.batchActions.hostIDs...)
			a.batchActions = nil
			return a, func() tea.Msg {
				return types.BatchCommandSubmitMsg{HostIDs: hostIDs, Command: command, ReadOnly: true}
			}
		}
		var cmd tea.Cmd
		a.batchActions.command, cmd = a.batchActions.command.Update(msg)
		return a, cmd
	default:
		switch msg.String() {
		case "esc":
			a.batchActions = nil
			return a, nil
		case "up", "k":
			a.batchActions.cursor = (a.batchActions.cursor - 1 + 3) % 3
			return a, nil
		case "down", "j":
			a.batchActions.cursor = (a.batchActions.cursor + 1) % 3
			return a, nil
		case "enter":
			hostIDs := append([]uint(nil), a.batchActions.hostIDs...)
			switch a.batchActions.cursor {
			case batchActionOpen:
				a.batchActions = nil
				return a, func() tea.Msg { return types.BatchActionSelectedMsg{HostIDs: hostIDs, Action: "open"} }
			case batchActionSnippet:
				a.batchActions = nil
				return a, func() tea.Msg { return types.BatchActionSelectedMsg{HostIDs: hostIDs, Action: "snippet"} }
			case batchActionReadOnly:
				a.batchActions.step = 1
				return a, a.batchActions.command.Focus()
			}
		}
	}
	return a, nil
}

func (b *batchActionsModel) paste(msg tea.PasteMsg) {
	if b.step == 1 {
		b.command = inputpaste.TextInput(b.command, msg)
	}
}

func (a *App) findSSHTabByHostID(hostID uint) *sshview.Model {
	for i := range a.tabs {
		if a.tabs[i].Type != SSHTab {
			continue
		}
		m, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok {
			continue
		}
		if m.HostID() == hostID {
			return m
		}
	}
	return nil
}
