package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/huangzheng2016/eTerm/internal/tmux"
)

// repaintActiveTab recovers a corrupted display at every layer: ClearScreen
// forces a full frame redraw, the raw 1049h re-enters alt screen when the
// outer terminal dropped out of it (bubbletea only emits it on transitions,
// which never happen here), and tmux refresh-client makes the server resend
// the full pane so a stale emulator grid gets overwritten.
func (a App) repaintActiveTab() (App, tea.Cmd) {
	cmds := []tea.Cmd{tea.Sequence(
		tea.ClearScreen,
		tea.Raw(ansi.SetModeAltScreenSaveCursor),
	)}
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
		if tab := a.tabs[a.activeTab]; tab.Type == LocalTab && tab.TmuxSession != "" {
			if configFile, err := a.resolveTmuxConfig(); err == nil {
				cmds = append(cmds, func() tea.Msg {
					_ = tmux.RefreshClient(context.Background(), configFile)
					return nil
				})
			}
		}
	}
	return a, tea.Batch(cmds...)
}
