package components

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type ToastType int

const (
	ToastInfo ToastType = iota
	ToastSuccess
	ToastError
	ToastWarning
)

type ToastModel struct {
	message   string
	toastType ToastType
	visible   bool
	seq       uint64
	timer     time.Time
	duration  time.Duration
}

type ToastTimeoutMsg struct {
	seq uint64
}

func NewToast() ToastModel {
	return ToastModel{}
}

// Dismiss hides the toast immediately (e.g. when a long-running action completes).
func (t ToastModel) Dismiss() ToastModel {
	t.visible = false
	return t
}

func (t ToastModel) Show(msg string, tt ToastType, duration time.Duration) (ToastModel, tea.Cmd) {
	t.message = msg
	t.toastType = tt
	t.visible = true
	t.seq++
	t.timer = time.Now()
	t.duration = duration
	seq := t.seq
	return t, tea.Tick(duration, func(_ time.Time) tea.Msg {
		return ToastTimeoutMsg{seq: seq}
	})
}

func (t ToastModel) Update(msg tea.Msg) (ToastModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ToastTimeoutMsg:
		if msg.seq == t.seq {
			t.visible = false
		}
	}
	return t, nil
}

func (t ToastModel) View() string {
	if !t.visible {
		return ""
	}

	var icon string
	var style lipgloss.Style
	switch t.toastType {
	case ToastSuccess:
		icon = "✓"
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(lipgloss.Color("#00c853"))
	case ToastError:
		icon = "✗"
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fff")).
			Background(lipgloss.Color("#d32f2f"))
	case ToastWarning:
		icon = "!"
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(lipgloss.Color("#ffab00"))
	default:
		icon = "~"
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e0e0e0")).
			Background(lipgloss.Color("#424242"))
	}

	badge := style.Bold(true).Padding(0, 1).Render(icon)
	text := lipgloss.NewStyle().Foreground(style.GetBackground()).Bold(true).Padding(0, 1).Render(t.message)

	return badge + text
}
