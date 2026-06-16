package app

import tea "charm.land/bubbletea/v2"

func (a *App) forwardToHomeTabs(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range a.tabs {
		if a.tabs[i].Type != HomeTab || a.tabs[i].Model == nil {
			continue
		}
		updated, cmd := a.tabs[i].Model.Update(msg)
		a.tabs[i].Model = updated
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}
