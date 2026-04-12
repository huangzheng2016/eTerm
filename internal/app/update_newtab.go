package app

import (
	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/editor"
	"github.com/eterm/eterm/internal/ui/fwdeditor"
	"github.com/eterm/eterm/internal/ui/fwdview"
	"github.com/eterm/eterm/internal/ui/keyview"
	"github.com/eterm/eterm/internal/ui/snippeteditor"
	"github.com/eterm/eterm/internal/ui/snippetview"

	tea "charm.land/bubbletea/v2"
)

func (a App) handleNewTabMsg(msg types.NewTabMsg) (App, tea.Cmd) {
	var tabModel tea.Model
	var initCmd tea.Cmd
	tabType := TabType(msg.Type)

	switch tabType {
	case EditorTab:
		var hostPtr *db.Host
		if msg.Data != nil {
			if h, ok := msg.Data.(db.Host); ok {
				hostPtr = &h
			}
		}
		em := editor.New(a.db, a.masterKey, hostPtr)
		if a.width > 0 {
			updated, _ := em.Update(tea.WindowSizeMsg{Width: a.width, Height: a.mainContentHeightForType(EditorTab)})
			if sized, ok := updated.(editor.Model); ok {
				em = sized
			}
		}
		tabModel = em
		initCmd = em.Init()
	case KeyTab:
		km := keyview.New(a.db, a.masterKey, BuildKeyViewKeys(a.kbConfig))
		if a.width > 0 {
			km.SetSize(a.width, a.mainContentHeightForType(KeyTab))
		}
		tabModel = km
		initCmd = km.Init()
	case ForwardTab:
		fm := fwdview.New(a.db, BuildFwdKeys(a.kbConfig))
		if a.width > 0 {
			fm.SetSize(a.width, a.mainContentHeightForType(ForwardTab))
		}
		tabModel = fm
		initCmd = fm.Init()
	case SnippetTab:
		sm := snippetview.New(a.db, BuildSnippetKeys(a.kbConfig))
		if a.width > 0 {
			sm.SetSize(a.width, a.mainContentHeightForType(SnippetTab))
		}
		tabModel = sm
		initCmd = sm.Init()
	case FwdEditorTab:
		var rule *db.PortForward
		if id, ok := msg.Data.(uint); ok && id > 0 {
			var r db.PortForward
			if err := a.db.First(&r, id).Error; err == nil {
				rule = &r
			}
		}
		fe := fwdeditor.New(a.db, rule)
		if a.width > 0 {
			fe.SetSize(a.width, a.mainContentHeightForType(FwdEditorTab))
		}
		tabModel = &fe
		initCmd = fe.Init()
	case SnippetEditorTab:
		var snippet *db.Snippet
		if id, ok := msg.Data.(uint); ok && id > 0 {
			var s db.Snippet
			if err := a.db.First(&s, id).Error; err == nil {
				snippet = &s
			}
		}
		se := snippeteditor.New(a.db, snippet)
		if a.width > 0 {
			se.SetSize(a.width, a.mainContentHeightForType(SnippetEditorTab))
		}
		tabModel = &se
		initCmd = se.Init()
	}

	tab := Tab{Type: tabType, Title: msg.Title, Model: tabModel}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, initCmd
}
