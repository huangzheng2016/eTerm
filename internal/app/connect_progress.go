package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

func (a App) beginConnectProgress(text string) (App, chan string, tea.Cmd, func(string)) {
	a.connectProgressSeq++
	seq := a.connectProgressSeq
	next := make(chan string, 16)
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show(text, components.ToastInfo, 30*time.Second)
	report := func(text string) {
		if text == "" {
			return
		}
		select {
		case next <- text:
		default:
		}
	}
	return a, next, tea.Batch(toastCmd, connectProgressCmd(seq, next), reflowWindow(a)), report
}

func connectStageText(prefix, stage string) string {
	return prefix + " - " + stage
}

func (a App) applyConnectProgress(msg connectProgressMsg) (App, tea.Cmd) {
	if msg.Seq != a.connectProgressSeq {
		return a, nil
	}
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show(msg.Text, components.ToastInfo, 30*time.Second)
	return a, tea.Batch(toastCmd, connectProgressCmd(msg.Seq, msg.Next), reflowWindow(a))
}

func (a App) stopConnectProgress() App {
	a.connectProgressSeq++
	a.toast = a.toast.Dismiss()
	return a
}

func connectProgressCmd(seq uint64, next <-chan string) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-next
		if !ok {
			return nil
		}
		return connectProgressMsg{Seq: seq, Text: text, Next: next}
	}
}
