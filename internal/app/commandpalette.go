package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"gorm.io/gorm"
)

type commandPaletteItem struct {
	Title    string
	Subtitle string
	Search   string
	Msg      tea.Msg
}

type commandPaletteModel struct {
	input    textinput.Model
	items    []commandPaletteItem
	filtered []commandPaletteItem
	cursor   int
	width    int
}

func newCommandPalette(items []commandPaletteItem) *commandPaletteModel {
	in := textinput.New()
	in.Placeholder = "Search commands, hosts, snippets..."
	in.CharLimit = 128
	p := &commandPaletteModel{input: in, items: items}
	p.refresh()
	return p
}

func newCommandPaletteFromDB(database *gorm.DB, width int) *commandPaletteModel {
	items := []commandPaletteItem{
		{Title: "Paste URL", Subtitle: "shell", Search: "paste upload clipboard file image url", Msg: types.PasteImageURLMsg{}},
		{Title: "Open Settings", Subtitle: "app", Search: "settings preferences keys", Msg: types.OpenSettingsMsg{}},
		{Title: "Voice Settings", Subtitle: "app", Search: "voice speech dictation microphone", Msg: openVoiceSettingsMsg{}},
		{Title: "Open Sync", Subtitle: "app", Search: "sync devices", Msg: types.OpenSyncMsg{}},
		{Title: "Open SSH Keys", Subtitle: "app", Search: "keys ssh", Msg: types.NewTabMsg{Type: string(KeyTab), Title: "Keys"}},
		{Title: "Open Snippets", Subtitle: "app", Search: "snippets commands", Msg: types.NewTabMsg{Type: string(SnippetTab), Title: "Snippets"}},
	}
	var hosts []db.Host
	_ = database.Order("alias, hostname").Find(&hosts).Error
	for _, h := range hosts {
		label := hostDisplayName(h)
		search := strings.Join([]string{label, h.Hostname, h.Username, h.Tags, h.Group}, " ")
		items = append(items,
			commandPaletteItem{Title: "Connect " + label, Subtitle: "host", Search: search + " connect ssh", Msg: types.SSHConnectMsg{HostID: h.ID}},
			commandPaletteItem{Title: "SFTP " + label, Subtitle: "host", Search: search + " sftp files", Msg: types.SFTPOpenMsg{HostID: h.ID}},
			commandPaletteItem{Title: "Edit " + label, Subtitle: "host", Search: search + " edit", Msg: types.NewTabMsg{Type: string(EditorTab), Title: "Edit", Data: h}},
			commandPaletteItem{Title: "Sessions " + label, Subtitle: "host", Search: search + " history sessions", Msg: types.OpenSessionHistoryMsg{HostID: h.ID}},
		)
	}
	var snippets []db.Snippet
	_ = database.Order("name").Find(&snippets).Error
	for _, s := range snippets {
		items = append(items, commandPaletteItem{
			Title:    "Snippet " + s.Name,
			Subtitle: "snippet",
			Search:   s.Name + " " + s.Command + " " + s.Tags,
			Msg:      types.SnippetSelectedMsg{Command: s.Command},
		})
	}
	p := newCommandPalette(items)
	p.width = width
	p.input.SetWidth(max(24, min(width-8, 64)))
	return p
}

func (p *commandPaletteModel) refresh() {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.filtered = p.filtered[:0]
	for _, item := range p.items {
		hay := strings.ToLower(item.Title + " " + item.Subtitle + " " + item.Search)
		if q == "" || strings.Contains(hay, q) {
			p.filtered = append(p.filtered, item)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *commandPaletteModel) selectedMsg() tea.Msg {
	if len(p.filtered) == 0 {
		return nil
	}
	return p.filtered[p.cursor].Msg
}

func (p *commandPaletteModel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil
	case "down", "ctrl+n":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refresh()
	return cmd
}

func (p *commandPaletteModel) paste(msg tea.PasteMsg) {
	p.input = inputpaste.TextInput(p.input, msg)
	p.refresh()
}

func (p *commandPaletteModel) View() string {
	var rows []string
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Command Palette")
	rows = append(rows, title, p.input.View(), "")
	limit := len(p.filtered)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		item := p.filtered[i]
		line := fmt.Sprintf("%s  %s", item.Title, lipgloss.NewStyle().Foreground(lipgloss.Color("#777")).Render(item.Subtitle))
		if i == p.cursor {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#5f5fff")).Render(line)
		}
		rows = append(rows, line)
	}
	if len(p.filtered) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("No matches"))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Enter run | Esc close"))
	return lipgloss.NewStyle().
		Width(max(32, min(p.width-4, 72))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
