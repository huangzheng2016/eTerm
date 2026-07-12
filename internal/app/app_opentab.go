package app

import (
	"encoding/json"

	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"github.com/huangzheng2016/eTerm/internal/ui/keyview"
	"github.com/huangzheng2016/eTerm/internal/ui/sessionhistview"
	"github.com/huangzheng2016/eTerm/internal/ui/settingsview"
	"github.com/huangzheng2016/eTerm/internal/ui/snippetview"
	"github.com/huangzheng2016/eTerm/internal/ui/syncview"

	tea "charm.land/bubbletea/v2"
)

func (a App) openKeysTab() (App, tea.Cmd) {
	for i := range a.tabs {
		if isListView(a.tabs[i].Type) {
			return a.activateListView(KeyTab)
		}
	}
	km := keyview.New(a.db, a.masterKey, BuildKeyViewKeys(a.kbConfig))
	if a.width > 0 {
		km.SetSize(a.width, a.mainContentHeightForType(KeyTab))
	}
	tab := Tab{Type: KeyTab, Title: "Keys", Model: km}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, km.Init()
}

func (a App) openForwardTab() (App, tea.Cmd) {
	for i := range a.tabs {
		if isListView(a.tabs[i].Type) {
			return a.activateListView(ForwardTab)
		}
	}
	fm := fwdview.New(a.db, BuildFwdKeys(a.kbConfig))
	if a.width > 0 {
		fm.SetSize(a.width, a.mainContentHeightForType(ForwardTab))
	}
	tab := Tab{Type: ForwardTab, Title: "Forwards", Model: fm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, fm.Init()
}

func (a App) openSnippetsTab() (App, tea.Cmd) {
	for i := range a.tabs {
		if isListView(a.tabs[i].Type) {
			return a.activateListView(SnippetTab)
		}
	}
	sm := snippetview.New(a.db, BuildSnippetKeys(a.kbConfig))
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(SnippetTab))
	}
	tab := Tab{Type: SnippetTab, Title: "Snippets", Model: sm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sm.Init()
}

func (a App) openSessionHistoryTab(hostID uint) (App, tea.Cmd) {
	sv := sessionhistview.New(a.db, hostID)
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(SessionHistoryTab))
	}
	tab := Tab{Type: SessionHistoryTab, Title: "Sessions", Model: sv}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sv.Init()
}

func (a App) openSettingsTab() (App, tea.Cmd) {
	// Check if settings tab already exists
	for i, tab := range a.tabs {
		if tab.Type == SettingsTab {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			return a, nil
		}
	}
	configData, _ := json.Marshal(a.kbConfig)
	defaultsData, _ := json.Marshal(DefaultKeyBindingConfig())
	sm := settingsview.New(a.db, configData, defaultsData, a.noPasswordMode)
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(SettingsTab))
	}
	tab := Tab{Type: SettingsTab, Title: "Settings", Model: sm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sm.Init()
}

func (a App) openSyncTab() (App, tea.Cmd) {
	for i, tab := range a.tabs {
		if tab.Type == SyncTab {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			return a, nil
		}
	}
	sm := syncview.New(a.db, a.masterKey)
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(SyncTab))
	}
	tab := Tab{Type: SyncTab, Title: "Sync", Model: sm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sm.Init()
}
