package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func (a App) refreshActiveHomeConnectivity() (App, tea.Cmd) {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return a, nil
	}
	if a.tabs[a.activeTab].Type != HomeTab || a.tabs[a.activeTab].Model == nil {
		return a, nil
	}
	updated, cmd := a.tabs[a.activeTab].Model.Update(types.RefreshConnectivityMsg{})
	a.tabs[a.activeTab].Model = updated
	return a, cmd
}
