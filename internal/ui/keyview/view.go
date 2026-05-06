package keyview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

var (
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

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
		return tea.NewView("Loading...")
	}

	help := helpStyle.Render("n:generate · i:import · e:export · d:delete · c:copy pubkey")

	if len(m.sshKeys) == 0 {
		block := lipgloss.JoinVertical(lipgloss.Center,
			"No SSH keys.",
			"",
			helpStyle.Render("Press 'n' to generate or 'i' to import."),
			"",
			help,
		)
		if m.width > 0 && m.height > 0 {
			bg := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
			return tea.NewView(m.maybeOverlay(bg))
		}
		return tea.NewView(m.maybeOverlay(block))
	}

	cards := make([]string, len(m.sshKeys))
	for i, k := range m.sshKeys {
		cards[i] = components.RenderCard(keyCardTitle(k.Name), keyCardDesc(k.Type, k.Fingerprint, k.CertificatePath), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.sshKeys), m.gridCursor, m.gridLayout)
	return tea.NewView(m.maybeOverlay(grid))
}

func (m Model) maybeOverlay(bg string) string {
	switch m.mode {
	case modeGenerate:
		overlay := m.renderGenerateOverlay()
		return m.placeOverlay(bg, overlay)
	case modeImport:
		overlay := m.renderImportOverlay()
		return m.placeOverlay(bg, overlay)
	}
	return bg
}

// PLACEHOLDER_OVERLAYS

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
