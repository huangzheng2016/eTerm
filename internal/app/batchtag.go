package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"gorm.io/gorm"
)

type batchTagModel struct {
	input textinput.Model
	ids   []uint
}

func newBatchTagModel(ids []uint) *batchTagModel {
	ti := textinput.New()
	ti.Placeholder = "tag to add (comma-separated ok)"
	ti.CharLimit = 256
	return &batchTagModel{input: ti, ids: append([]uint(nil), ids...)}
}

func (b *batchTagModel) syncWidth(termW int) {
	iw := 50
	if termW > 0 {
		iw = max(32, termW-16)
		if iw > 78 {
			iw = 78
		}
	}
	b.input.SetWidth(iw)
}

func (b *batchTagModel) View() string {
	title := ui.TitleStyle.Render("Batch tag")
	sub := ui.DimStyle.Render(fmt.Sprintf("%d host(s) selected", len(b.ids)))
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Enter apply · Esc cancel · click left apply / right cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", sub, "", b.input.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

func (a App) handleBatchTagKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.batchTag = nil
		return a, nil
	case "enter":
		raw := strings.TrimSpace(a.batchTag.input.Value())
		ids := append([]uint(nil), a.batchTag.ids...)
		a.batchTag = nil
		if raw == "" || len(ids) == 0 {
			return a, nil
		}
		return a, func() tea.Msg {
			return batchTagApplyMsg{HostIDs: ids, RawTags: raw}
		}
	}
	var cmd tea.Cmd
	a.batchTag.input, cmd = a.batchTag.input.Update(msg)
	return a, cmd
}

func (b *batchTagModel) paste(msg tea.PasteMsg) {
	b.input = inputpaste.TextInput(b.input, msg)
}

type batchTagApplyMsg struct {
	HostIDs []uint
	RawTags string
}

func applyBatchTags(database *gorm.DB, msg batchTagApplyMsg) tea.Msg {
	parts := strings.Split(msg.RawTags, ",")
	var add []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			add = append(add, t)
		}
	}
	if len(add) == 0 {
		return types.SuccessMsg{Message: "No tags to add"}
	}
	for _, id := range msg.HostIDs {
		var h db.Host
		if err := database.First(&h, id).Error; err != nil {
			continue
		}
		existing := parseTagsString(h.Tags)
		seen := map[string]bool{}
		for _, t := range existing {
			seen[strings.ToLower(t)] = true
		}
		for _, t := range add {
			if !seen[strings.ToLower(t)] {
				existing = append(existing, t)
				seen[strings.ToLower(t)] = true
			}
		}
		h.Tags = strings.Join(existing, ", ")
		_ = database.Model(&h).Updates(map[string]interface{}{"tags": h.Tags}).Error
	}
	return types.SuccessMsg{Message: "Tags updated"}
}

func parseTagsString(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
