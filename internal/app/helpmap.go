package app

import (
	"strings"

	bubbleshelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// statusBarShortcutParts builds the bottom status line. Tab-specific bindings (C-t, C-S-b, C-p)
// and lock/snippet-picker are in the ? FullHelp overlay, not here — except on SSH session tabs
// where ? is not available, so lock + snippet picker stay on the bar.
func statusBarShortcutParts(km KeyMap, sshDisconnected bool, sshSession bool) []string {
	parts := []string{
		km.QuitApp.Help().Key + " quit",
		km.CloseTabSafe.Help().Key + " close tab",
		km.NextTab.Help().Key + " next",
		km.PrevTab.Help().Key + " prev",
	}
	if sshSession {
		parts = append(parts,
			km.LockApp.Help().Key+" lock",
			"C-S-s snippet",
		)
	}
	if sshDisconnected {
		parts = append(parts, "r reconnect")
	}
	return parts
}

// sshStatusBarHint is the status line while the active tab is an SSH terminal.
func sshStatusBarHint(km KeyMap, sshDisconnected bool) string {
	return strings.Join(statusBarShortcutParts(km, sshDisconnected, true), " · ")
}

// mainViewStatusBarHint is what MainView shows on the bottom status line for every tab type.
// SSH uses sshStatusBarHint only (? stays with the remote); other tabs append a ? overlay hint.
func mainViewStatusBarHint(km KeyMap, tabType TabType, sshDisconnected bool) string {
	if tabType == SSHTab {
		return sshStatusBarHint(km, sshDisconnected)
	}
	return strings.Join(statusBarShortcutParts(km, false, false), " · ") + " · ? all keys"
}

// homeHelp* are list-only bindings for FullHelp / ShortHelp where needed.
var (
	homeHelpSnippet   = key.NewBinding(key.WithKeys("ctrl+shift+s"), key.WithHelp("C-S-s", "snippet"))
	homeHelpCopy      = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy"))
	homeHelpQuick     = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quick"))
	homeHelpImport    = key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "import .ssh"))
	homeHelpExport    = key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "export"))
	homeHelpTagToggle = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "group/tag"))
	homeHelpClone     = key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "clone host"))
	homeHelpHidden    = key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "show hidden"))
	homeHelpHide      = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hide/unhide"))

	// SFTP-specific bindings (shown in ? overlay for SFTP tabs)
	sftpHelpUpload   = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upload"))
	sftpHelpDownload = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download"))
	sftpHelpDelete   = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete"))
	sftpHelpMkdir    = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mkdir"))
	sftpHelpRename   = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename"))
	sftpHelpPanel    = key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "switch panel"))
)

// homeAppHelpMap: FullHelp only (? overlay); ShortHelp unused — global hints are mainViewStatusBarHint.
type homeAppHelpMap struct{ km KeyMap }

func (h homeAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h homeAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, homeHelpSnippet, homeHelpTagToggle},
		{h.km.SSHConnect, h.km.SFTPOpen, h.km.NewHost},
		{h.km.EditHost, h.km.DeleteHost, h.km.Search},
		{homeHelpCopy, homeHelpClone, homeHelpQuick, homeHelpImport},
		{homeHelpExport, homeHelpTagToggle, homeHelpHide, homeHelpHidden},
	}
}

type sftpAppHelpMap struct{ km KeyMap }

func (h sftpAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h sftpAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.LockApp, homeHelpSnippet},
		{sftpHelpUpload, sftpHelpDownload, sftpHelpDelete},
		{sftpHelpMkdir, sftpHelpRename, sftpHelpPanel},
	}
}

type editorAppHelpMap struct{ km KeyMap }

func (h editorAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h editorAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, homeHelpSnippet},
	}
}

type keyTabAppHelpMap struct{ km KeyMap }

func (h keyTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h keyTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, homeHelpSnippet},
	}
}

type forwardTabAppHelpMap struct{ km KeyMap }

func (h forwardTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h forwardTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, homeHelpSnippet},
	}
}

type snippetTabAppHelpMap struct{ km KeyMap }

func (h snippetTabAppHelpMap) ShortHelp() []key.Binding { return nil }

func (h snippetTabAppHelpMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{h.km.QuitApp, h.km.NewTab, h.km.CloseTabSafe},
		{h.km.SnippetsTab, h.km.ForwardTab, h.km.NextTab, h.km.PrevTab},
		{h.km.CloseTab, h.km.LockApp, homeHelpSnippet},
	}
}

type emptyHelpMap struct{}

func (emptyHelpMap) ShortHelp() []key.Binding { return nil }

func (emptyHelpMap) FullHelp() [][]key.Binding { return nil }

// fullHelpHasAnyBinding reports whether FullHelp has at least one enabled binding (for ? overlay).
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
		return homeAppHelpMap{a.keyMap}
	case SSHTab:
		// FullHelp empty; shortcuts only on status bar (sshStatusBarHint).
		return emptyHelpMap{}
	case SFTPTab:
		return sftpAppHelpMap{a.keyMap}
	case EditorTab:
		return editorAppHelpMap{a.keyMap}
	case KeyTab:
		return keyTabAppHelpMap{a.keyMap}
	case ForwardTab:
		return forwardTabAppHelpMap{a.keyMap}
	case FwdEditorTab:
		return editorAppHelpMap{a.keyMap}
	case SnippetTab:
		return snippetTabAppHelpMap{a.keyMap}
	case SnippetEditorTab:
		return editorAppHelpMap{a.keyMap}
	default:
		return emptyHelpMap{}
	}
}
