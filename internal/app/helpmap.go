package app

import (
	"strings"

	bubbleshelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// statusBarShortcutParts builds the bottom status line.
func statusBarShortcutParts(km KeyMap, cfg KeyBindingConfig, sshDisconnected bool, sshSession bool, detachable bool) []string {
	closeLabel := " close tab"
	if detachable {
		closeLabel = " detach"
	}
	parts := []string{
		km.QuitApp.Help().Key + " quit",
		km.LocalTerminal.Help().Key + " local",
		km.RenameTab.Help().Key + " rename",
		km.CloseTabSafe.Help().Key + closeLabel,
		km.NextTab.Help().Key + " next",
		km.PrevTab.Help().Key + " prev",
	}
	if sshSession {
		parts = append(parts,
			km.LockApp.Help().Key+" lock",
			helpLabel(cfg.SSHSnippetPicker)+" snippet",
			km.PasteImageURL.Help().Key+" image",
		)
	}
	if sshDisconnected {
		parts = append(parts, helpLabel(cfg.SSHReconnect)+" reconnect")
	}
	return parts
}

func sshStatusBarHint(km KeyMap, cfg KeyBindingConfig, sshDisconnected bool, detachable bool) string {
	return strings.Join(statusBarShortcutParts(km, cfg, sshDisconnected, true, detachable), " · ")
}

func mainViewStatusBarHint(km KeyMap, cfg KeyBindingConfig, tabType TabType, sshDisconnected bool, detachable bool) string {
	switch tabType {
	case SSHTab, LocalTab:
		return sshStatusBarHint(km, cfg, sshDisconnected, detachable)
	case HomeTab:
		return homeStatusBarHint(km, cfg)
	case SnippetTab:
		return snippetStatusBarHint(km, cfg)
	}
	return strings.Join(statusBarShortcutParts(km, cfg, false, false, false), " · ") + " · " + helpLabel(cfg.Help) + " all keys"
}

// homeStatusBarHint shows the host-list shortcuts inline so they are visible without opening full help.
func homeStatusBarHint(km KeyMap, cfg KeyBindingConfig) string {
	parts := []string{
		km.SSHConnect.Help().Key + " connect",
		km.NewHost.Help().Key + " new",
		km.EditHost.Help().Key + " edit",
		km.DeleteHost.Help().Key + " delete",
		km.Search.Help().Key + " search",
		helpLabel(cfg.TmuxMenu) + " tmux",
		km.RenameTab.Help().Key + " rename",
		helpLabel(cfg.ShowHidden) + " show hidden",
		helpLabel(cfg.HideHost) + " hide",
		helpLabel(cfg.Help) + " all keys",
	}
	return strings.Join(parts, " · ")
}

func snippetStatusBarHint(km KeyMap, cfg KeyBindingConfig) string {
	parts := []string{
		helpLabel(cfg.SnipNew) + " new",
		helpLabel(cfg.SnipEdit) + " edit",
		helpLabel(cfg.SnipDelete) + " delete",
		helpLabel(cfg.SnippetPicker) + " run",
		helpLabel(cfg.Help) + " all keys",
	}
	return strings.Join(parts, " · ")
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
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet"), dynamicBinding(h.cfg.ToggleView, "group/tag"), dynamicBinding(h.cfg.TmuxMenu, "tmux")},
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
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
		{dynamicBinding(h.cfg.SFTPUpload, "upload"), dynamicBinding(h.cfg.SFTPDownload, "download"), dynamicBinding(h.cfg.SFTPDelete, "delete")},
		{dynamicBinding(h.cfg.SFTPMkdir, "mkdir"), dynamicBinding(h.cfg.SFTPRename, "rename"), dynamicBinding(h.cfg.SFTPChmod, "chmod")},
		{dynamicBinding(h.cfg.SFTPSwitchLeft, "left panel"), dynamicBinding(h.cfg.SFTPSwitchRight, "right panel")},
	}
}

type editorAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h editorAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h editorAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
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
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
		{dynamicBinding(h.cfg.KeyNew, "new key"), dynamicBinding(h.cfg.KeyImport, "import key"), dynamicBinding(h.cfg.KeyExport, "export key")},
		{dynamicBinding(h.cfg.KeyDelete, "delete key"), dynamicBinding(h.cfg.KeyCopy, "copy pubkey")},
	}
}

type forwardTabAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h forwardTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h forwardTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
		{dynamicBinding(h.cfg.FwdNew, "new rule"), dynamicBinding(h.cfg.FwdEdit, "edit rule"), dynamicBinding(h.cfg.FwdDelete, "delete rule")},
		{dynamicBinding(h.cfg.FwdStart, "start"), dynamicBinding(h.cfg.FwdStop, "stop")},
	}
}

type snippetTabAppHelpMap struct {
	km  KeyMap
	cfg KeyBindingConfig
}

func (h snippetTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h snippetTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.LocalTerminal, h.km.RenameTab, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, dynamicBinding(h.cfg.SnippetPicker, "snippet")},
		{dynamicBinding(h.cfg.SnipNew, "new snippet"), dynamicBinding(h.cfg.SnipEdit, "edit snippet"), dynamicBinding(h.cfg.SnipDelete, "delete snippet")},
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
	case SSHTab, LocalTab:
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
