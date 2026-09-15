package app

import (
	"encoding/json"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"github.com/huangzheng2016/eTerm/internal/ui/keyview"
	"github.com/huangzheng2016/eTerm/internal/ui/sessionhistview"
	"github.com/huangzheng2016/eTerm/internal/ui/sessionlistview"
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
	sv.SetShowEmptyKeys(a.kbConfig.ShowHidden)
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(SessionHistoryTab))
	}
	tab := Tab{Type: SessionHistoryTab, Title: "Sessions", Model: sv}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sv.Init()
}

func (a App) openSessionReplayTab(historyID uint, title string) (App, tea.Cmd) {
	m := sessionlistview.NewReplay(a.db, historyID)
	if a.width > 0 {
		m.SetSize(a.width, a.mainContentHeightForType(SessionReplayTab))
	}
	if title == "" {
		title = "Session"
	}
	tab := Tab{Type: SessionReplayTab, Title: "Replay: " + title, Model: m}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, m.Init()
}

func (a App) openSettingsTab() (App, tea.Cmd) {
	for i, tab := range a.tabs {
		if tab.Type == SettingsTab {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			return a, nil
		}
	}
	sm := settingsview.New(a.db, a.noPasswordMode)
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(SettingsTab))
	}
	tab := Tab{Type: SettingsTab, Title: "Settings", Model: sm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sm.Init()
}

func (a App) openShortcutsTab() (App, tea.Cmd) {
	for i, tab := range a.tabs {
		if tab.Type == ShortcutsTab {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			return a, nil
		}
	}
	configData, _ := json.Marshal(a.kbConfig)
	if raw, err := db.GetSetting(a.db, keybindingsSettingKey); err == nil && raw != "" {
		var extra map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &extra) == nil {
			var known map[string]json.RawMessage
			_ = json.Unmarshal(configData, &known)
			for k, v := range extra {
				if _, ok := known[k]; !ok {
					known[k] = v
				}
			}
			if merged, err := json.Marshal(known); err == nil {
				configData = merged
			}
		}
	}
	defaultsData, _ := json.Marshal(DefaultKeyBindingConfig())
	sm := settingsview.NewShortcuts(a.db, configData, defaultsData)
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(ShortcutsTab))
	}
	tab := Tab{Type: ShortcutsTab, Title: "Shortcuts", Model: sm}
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

func (a App) openVoiceSettingsTab(fromHotkey bool) (App, tea.Cmd) {
	for i, tab := range a.tabs {
		if tab.Type == VoiceTab {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			if m, ok := tab.Model.(*voiceSettingsModel); ok && fromHotkey {
				m.fromHotkey = true
			}
			return a, nil
		}
	}
	a = a.ensureVoiceCfg()
	m := newVoiceSettingsModel(a.db, a.masterKey, a.voiceCfg)
	m.fromHotkey = fromHotkey
	if a.width > 0 {
		m.SetSize(a.width, a.mainContentHeightForType(VoiceTab))
	}
	tab := Tab{Type: VoiceTab, Title: "Voice", Model: m}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, m.Init()
}

type openShortcutsMsg struct{}
