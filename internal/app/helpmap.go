package app

import (
	"strings"

	bubbleshelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// statusBarShortcutParts builds the bottom status line.
func statusBarShortcutParts(km KeyMap, cfg KeyBindingConfig, sshDisconnected bool, sshSession bool) []string {
	parts := []string{
		km.QuitApp.Help().Key + " quit",
		km.CloseTabSafe.Help().Key + " close tab",
		km.NextTab.Help().Key + " next",
		km.PrevTab.Help().Key + " prev",
	}
	if sshSession {
		parts = append(parts,
			km.LockApp.Help().Key+" lock",
			helpLabel(cfg.SSHSnippetPicker)+" snippet",
		)
	}
	if sshDisconnected {
		parts = append(parts, helpLabel(cfg.SSHReconnect)+" reconnect")
	}
	return parts
}

func sshStatusBarHint(km KeyMap, cfg KeyBindingConfig, sshDisconnected bool) string {
	return strings.Join(statusBarShortcutParts(km, cfg, sshDisconnected, true), " · ")
}

func mainViewStatusBarHint(km KeyMap, cfg KeyBindingConfig, tabType TabType, sshDisconnected bool) string {
	if tabType == SSHTab {
		return sshStatusBarHint(km, cfg, sshDisconnected)
	}
	return strings.Join(statusBarShortcutParts(km, cfg, false, false), " · ") + " · ? all keys"
}

// dynamicBinding creates a key.Binding from config keys for help display.
func dynamicBinding(keys []string, helpDesc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(helpLabel(keys), helpDesc))
}

type homeAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h homeAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h homeAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet"), dynamicBinding(h.cfg.ToggleView, "group/tag")},
		{h.km.SSHConnect, h.km.SFTPOpen, h.km.NewHost},
		{h.km.EditHost, h.km.DeleteHost, h.km.Search},
		{dynamicBinding(h.cfg.CopySSH, "copy"), dynamicBinding(h.cfg.CloneHost, "clone"), dynamicBinding(h.cfg.QuickConnect, "quick"), dynamicBinding(h.cfg.ImportSSH, "import .ssh")},
		{dynamicBinding(h.cfg.ExportConfig, "export"), dynamicBinding(h.cfg.HideHost, "hide/unhide"), dynamicBinding(h.cfg.ShowHidden, "show hidden")},
		{dynamicBinding(h.cfg.SessionHistory, "session log"), dynamicBinding(h.cfg.ToggleSelect, "toggle select"), dynamicBinding(h.cfg.BatchTag, "batch tag")},
		{dynamicBinding(h.cfg.BatchActions, "batch actions")},
	}
}

type sftpAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h sftpAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h sftpAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
		{dynamicBinding(h.cfg.SFTPUpload, "upload"), dynamicBinding(h.cfg.SFTPDownload, "download"), dynamicBinding(h.cfg.SFTPDelete, "delete")},
		{dynamicBinding(h.cfg.SFTPMkdir, "mkdir"), dynamicBinding(h.cfg.SFTPRename, "rename"), dynamicBinding(h.cfg.SFTPChmod, "chmod")},
		{dynamicBinding(h.cfg.SFTPSwitchLeft, "left panel")},
	}
}

type editorAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h editorAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h editorAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
	}
}

type keyTabAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h keyTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h keyTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
	}
}

type forwardTabAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h forwardTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h forwardTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
	}
}

type snippetTabAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h snippetTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h snippetTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
	}
}

type emptyHelpMap struct{}

func (emptyHelpMap) ShortHelp() []key.Binding { return nil }

func (emptyHelpMap) FullHelp() [][]key.Binding { return nil }

func fullHelpHasAnyBinding(k bubbleshelp.KeyMap) bool {
	for _, g := range k.FullHelp() {
		for _, b := range g {
			if b.Enabled() {
				return true
			}
		}
	}
	return false
}

func (a App) contextualHelpKeyMap() bubbleshelp.KeyMap {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return emptyHelpMap{}
	}
	switch a.tabs[a.activeTab].Type {
	case HomeTab:
		return homeAppHelpMap{a.keyMap, a.kbConfig}
	case SSHTab:
		return emptyHelpMap{}
	case SFTPTab:
		return sftpAppHelpMap{a.keyMap, a.kbConfig}
	case EditorTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case KeyTab:
		return keyTabAppHelpMap{a.keyMap, a.kbConfig}
	case ForwardTab:
		return forwardTabAppHelpMap{a.keyMap, a.kbConfig}
	case FwdEditorTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case SnippetTab:
		return snippetTabAppHelpMap{a.keyMap, a.kbConfig}
	case SnippetEditorTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case SettingsTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case SyncTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case SessionHistoryTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	case BatchResultTab:
		return editorAppHelpMap{a.keyMap, a.kbConfig}
	default:
		return emptyHelpMap{}
	}
}
