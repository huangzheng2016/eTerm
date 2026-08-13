package keyview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

var (
	overlayBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3)

	overlayTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7D56F4")).
				MarginBottom(1)

	overlayHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#626262")).
				MarginTop(1)
)

func keyCardTitle(name string) string {
	return name
}

func keyCardDesc(keyType, fingerprint, certificatePath string) string {
	fp := fingerprint
	if len(fp) > 20 {
		fp = fp[:20] + "…"
	}
	if certificatePath != "" {
		return fmt.Sprintf("%s cert %s", keyType, fp)
	}
	return fmt.Sprintf("%s %s", keyType, fp)
}

func (m Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(components.Loading(m.width, m.height, "Loading..."))
	}

	if len(m.sshKeys) == 0 {
		bg := components.EmptyState(m.width, m.height,
			"No SSH keys.",
			"Press '"+viewkeys.HelpLabel(m.vk.New)+"' to generate or '"+viewkeys.HelpLabel(m.vk.Import)+"' to import.",
			keyHelpLine(m.vk),
		)
		return tea.NewView(m.maybeOverlay(bg))
	}

	cards := make([]string, len(m.sshKeys))
	start, end := components.GridPageRange(len(m.sshKeys), m.gridCursor, m.gridLayout)
	for i := start; i < end; i++ {
		k := m.sshKeys[i]
		cards[i] = components.RenderCard(keyCardTitle(k.Name), keyCardDesc(k.Type, k.Fingerprint, k.CertificatePath), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.sshKeys), m.gridCursor, m.gridLayout)
	return tea.NewView(m.maybeOverlay(grid))
}

func keyHelpLine(vk viewkeys.KeyViewKeys) string {
	return viewkeys.HelpLabel(vk.New) + ":generate · " +
		viewkeys.HelpLabel(vk.Import) + ":import · " +
		viewkeys.HelpLabel(vk.Edit) + ":edit · " +
		viewkeys.HelpLabel(vk.Delete) + ":delete · " +
		"enter:details"
}

func (m Model) maybeOverlay(bg string) string {
	switch m.mode {
	case modeGenerate:
		overlay := m.renderGenerateOverlay()
		return m.placeOverlay(bg, overlay)
	case modeImport:
		overlay := m.renderImportOverlay()
		return m.placeOverlay(bg, overlay)
	case modeDelete:
		overlay := m.renderDeleteOverlay()
		return m.placeOverlay(bg, overlay)
	case modeDetail:
		return m.placeOverlay(bg, m.renderDetailOverlay())
	case modeEdit:
		return m.placeOverlay(bg, m.renderEditOverlay())
	}
	return bg
}

func (m Model) renderDetailOverlay() string {
	k := m.keyByID(m.activeKeyID)
	if k == nil {
		return ""
	}
	pubkey := strings.TrimSpace(k.PublicKeyData)
	if len(pubkey) > 96 {
		pubkey = pubkey[:96] + "..."
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		overlayTitleStyle.Render(k.Name),
		"Type: "+k.Type,
		"Storage: "+k.StorageMode,
		"Fingerprint: "+k.Fingerprint,
		"Public key:",
		ui.DimStyle.Render(pubkey),
		"",
		ui.SelectedStyle.Render("["+viewkeys.HelpLabel(m.vk.Copy)+" Copy public key]")+"  "+ui.SelectedStyle.Render("["+viewkeys.HelpLabel(m.vk.Edit)+" Edit]"),
		overlayHintStyle.Render("Esc: close"),
	)
	return overlayBoxStyle.Width(m.overlayWidth()).Render(content)
}

func (m Model) renderEditOverlay() string {
	label, input, hint := "Name:", m.nameInput.View(), "Enter: next  Esc: cancel"
	if m.step == 1 {
		label, input, hint = "Certificate path:", m.certPathInput.View(), "Enter: save  Esc: cancel"
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		overlayTitleStyle.Render("Edit SSH Key"),
		label,
		input,
		overlayHintStyle.Render(hint),
	)
	return overlayBoxStyle.Width(m.overlayWidth()).Render(content)
}

// PLACEHOLDER_OVERLAYS

func (m Model) renderDeleteOverlay() string {
	title := overlayTitleStyle.Render("Delete SSH Key")
	name := lipgloss.NewStyle().Bold(true).Render(m.pendingDeleteName)
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"Delete key "+name+"?",
		"",
		overlayHintStyle.Render("y: confirm  n/esc: cancel"),
	)
	return overlayBoxStyle.Width(m.overlayWidth()).Render(content)
}

func (m Model) overlayWidth() int {
	w := m.width - 8
	if w < 32 {
		w = 32
	}
	if w > 78 {
		w = 78
	}
	return w
}

func (m Model) renderGenerateOverlay() string {
	title := overlayTitleStyle.Render("Generate SSH Key")
	boxW := m.overlayWidth()

	if m.step == 0 {
		content := lipgloss.JoinVertical(lipgloss.Left,
			title,
			"Name:",
			m.nameInput.View(),
			overlayHintStyle.Render("Enter: next  Esc: cancel"),
		)
		return overlayBoxStyle.Width(boxW).Render(content)
	}

	typeDisplay := lipgloss.NewStyle().Render("◀ " + m.typeOptions[m.typeIdx] + " ▶")
	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"Key Type:",
		typeDisplay,
		overlayHintStyle.Render("←→: select type  Enter: generate  Esc: cancel"),
	)
	return overlayBoxStyle.Width(boxW).Render(content)
}

func (m Model) renderImportOverlay() string {
	title := overlayTitleStyle.Render("Import SSH Key")
	boxW := m.overlayWidth()

	if m.step == 0 {
		content := lipgloss.JoinVertical(lipgloss.Left,
			title,
			"Name:",
			m.nameInput.View(),
			overlayHintStyle.Render("Enter: next  Esc: cancel"),
		)
		return overlayBoxStyle.Width(boxW).Render(content)
	}

	if m.step == 1 {
		content := lipgloss.JoinVertical(lipgloss.Left,
			title,
			"Certificate path (optional):",
			m.certPathInput.View(),
			overlayHintStyle.Render("Enter: next  Esc: cancel"),
		)
		return overlayBoxStyle.Width(boxW).Render(content)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"Paste private key (PEM) or file path:",
		m.keyPaste.View(),
		overlayHintStyle.Render("Ctrl+Enter: import  Esc: cancel"),
	)
	return overlayBoxStyle.Width(boxW).Render(content)
}

func (m Model) placeOverlay(bg, overlay string) string {
	if m.width > 0 && m.height > 0 {
		bgLines := strings.Count(bg, "\n") + 1
		if bgLines < m.height {
			bg += strings.Repeat("\n", m.height-bgLines)
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "))
	}
	return overlay
}
