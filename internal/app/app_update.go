package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"github.com/huangzheng2016/eTerm/internal/ui/batchresultview"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"github.com/huangzheng2016/eTerm/internal/ui/home"
	"github.com/huangzheng2016/eTerm/internal/ui/keyview"
	"github.com/huangzheng2016/eTerm/internal/ui/remotemenu"
	"github.com/huangzheng2016/eTerm/internal/ui/settingsview"
	"github.com/huangzheng2016/eTerm/internal/ui/sftpview"
	"github.com/huangzheng2016/eTerm/internal/ui/shareview"
	"github.com/huangzheng2016/eTerm/internal/ui/snippetview"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/ui/tmuxmenu"
	"github.com/huangzheng2016/eTerm/internal/version"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
	"github.com/huangzheng2016/eTerm/internal/voice"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Agent events, spinner ticks and size changes reach the AI overlay even
	// while it is hidden; interactive input goes through the chains below.
	if a.aiView != nil && !aiSkipForward(msg) {
		if cmd := a.updateAIView(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.tabBar = a.tabBar.SetWidth(a.width)
		a.statusBar = a.statusBar.SetWidth(a.width)
		if a.quickConnect != nil {
			a.quickConnect.syncInputWidth(a.width)
		}
		if a.batchTag != nil {
			a.batchTag.syncWidth(a.width)
		}
		if a.batchActions != nil {
			a.batchActions.syncWidth(a.width)
		}
		if a.renamePrompt != nil {
			a.renamePrompt.syncWidth(a.width)
		}
		if a.sharePrompt != nil {
			a.sharePrompt.SetWidth(a.width)
		}
		if a.importHostList != nil {
			a.importHostList.setPageSize(a.height)
		}
		if a.importKeyList != nil {
			a.importKeyList.setPageSize(a.height)
		}
		var layoutCmd tea.Cmd
		a, layoutCmd = layoutTabModels(a)
		cmds = append(cmds, layoutCmd)
		if a.loginModel != nil {
			updated, cmd := a.loginModel.Update(msg)
			a.loginModel = updated
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case sshview.ChunkMsg:
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			if m, ok := a.tabs[a.activeTab].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[a.activeTab].Model = updated
				return a, cmd
			}
		}
		for i := range a.tabs {
			if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[i].Model = updated
				return a, cmd
			}
		}
		return a, nil

	case sshview.StreamDoneMsg:
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			if m, ok := a.tabs[a.activeTab].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[a.activeTab].Model = updated
				return a, cmd
			}
		}
		for i := range a.tabs {
			if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[i].Model = updated
				return a, cmd
			}
		}
		return a, nil

	case sshview.TitleMsg:
		for i := range a.tabs {
			m, ok := a.tabs[i].Model.(*sshview.Model)
			if !ok || m.StreamID() != msg.StreamID {
				continue
			}
			if !a.tabs[i].userRenamed && msg.Title != "" && a.tabs[i].Title != msg.Title {
				a.tabs[i].Title = msg.Title
				a.syncTabBar()
			}
			return a, nil
		}
		return a, nil

	case components.ToastTimeoutMsg:
		a.toast, _ = a.toast.Update(msg)
		if a.viewState != MainView || len(a.tabs) == 0 {
			return a, nil
		}
		var layoutCmd tea.Cmd
		a, layoutCmd = layoutTabModels(a)
		return a, tea.Batch(layoutCmd, reflowWindow(a))

	case batchresultview.HostStartMsg, batchresultview.HostOutputMsg, batchresultview.HostDoneMsg, batchresultview.AllDoneMsg:
		for i := range a.tabs {
			m, ok := a.tabs[i].Model.(*batchresultview.Model)
			if !ok {
				continue
			}
			var jobID uint64
			switch v := msg.(type) {
			case batchresultview.HostStartMsg:
				jobID = v.JobID
			case batchresultview.HostOutputMsg:
				jobID = v.JobID
			case batchresultview.HostDoneMsg:
				jobID = v.JobID
			case batchresultview.AllDoneMsg:
				jobID = v.JobID
			}
			if m.JobID() != jobID {
				continue
			}
			updated, cmd := m.Update(msg)
			a.tabs[i].Model = updated
			return a, cmd
		}
		return a, nil

	case quickConnectFingerprintMsg:
		a = a.stopConnectProgress()
		a.pendingQuickConnect = &msg.info
		a.pendingFingerprint = &msg.confirmInfo
		fp := msg.confirmInfo
		a.confirm = components.NewConfirm(fingerprintConfirmTitle(fp), fingerprintConfirmBody(fp)).Show()
		return a, nil

	case connectProgressMsg:
		return a.applyConnectProgress(msg)

	case tea.KeyPressMsg:
		if a.confirm.IsActive() {
			wasActive := true
			var cmd tea.Cmd
			a.confirm, cmd = a.confirm.Update(msg)
			cmds = append(cmds, cmd)
			if wasActive && !a.confirm.IsActive() {
				actionCmd := a.processConfirmResult()
				if actionCmd != nil {
					cmds = append(cmds, actionCmd)
				}
			}
			return a, tea.Batch(cmds...)
		}

		// Quick connect overlay intercepts all keys when active
		if a.quickConnect != nil {
			return a.handleQuickConnectKey(msg)
		}

		if a.connError != nil {
			return a.handleConnErrorKey(msg)
		}

		if a.commandPalette != nil {
			switch msg.String() {
			case "esc", "escape":
				a.commandPalette = nil
				return a, nil
			case "enter":
				selected := a.commandPalette.selectedMsg()
				a.commandPalette = nil
				if selected == nil {
					return a, nil
				}
				return a, func() tea.Msg { return selected }
			}
			return a, a.commandPalette.Update(msg)
		}

		if a.voiceSettingsView != nil {
			closed, cmd := a.voiceSettingsView.Update(msg)
			if closed {
				a.voiceSettingsView = nil
				var tc tea.Cmd
				a, tc = a.endVoiceTest()
				return a, tea.Batch(cmd, tc)
			}
			return a, cmd
		}

		// Voice toggle works in every tab and inside the AI overlay. The
		// Settings tab keeps the key for its own reset-to-defaults.
		if a.viewState == MainView && !a.activeTabIsSettings() && key.Matches(msg, a.keyMap.VoiceInput) {
			return a.toggleVoice()
		}

		// AI overlay intercepts all keys when visible; esc emits aiview.CloseMsg.
		if a.aiVisible && a.aiView != nil {
			if key.Matches(msg, a.keyMap.AIOverlay) {
				// The open key toggles the overlay closed; it never reaches the
				// panel, so it cannot kill the input draft or the session.
				a.aiVisible = false
				return a, nil
			}
			cmd := a.updateAIView(msg)
			return a, cmd
		}

		if a.renamePrompt != nil {
			closed, cmd := a.renamePrompt.Update(msg)
			if closed {
				a.renamePrompt = nil
			}
			return a, cmd
		}

		if a.sharePrompt != nil {
			closed, cmd := a.sharePrompt.Update(msg)
			if closed {
				a.sharePrompt = nil
			}
			return a, cmd
		}

		if a.remoteMenu != nil {
			closed, cmd := a.remoteMenu.Update(msg)
			if closed {
				a.remoteMenu = nil
			}
			return a, cmd
		}

		if a.tmuxMenu != nil {
			closed, cmd := a.tmuxMenu.Update(msg)
			if closed {
				a.tmuxMenu = nil
			}
			return a, cmd
		}

		if a.batchTag != nil {
			return a.handleBatchTagKey(msg)
		}

		if a.batchActions != nil {
			return a.handleBatchActionsKey(msg)
		}

		if a.importKeyList != nil {
			closed, confirmed, cmd := a.importKeyList.Update(msg)
			if confirmed {
				if a.importHostList.exportMode {
					return a, runSelectedExport(a.db, a.importHostList.items)
				}
				if a.importHostList.sshSource {
					return a, runSSHListImport(a.db, a.importHostList.items, a.importKeyList.items)
				}
				return a, runTermiusImport(a.db, a.masterKey, a.importHostList.items, a.importKeyList.items)
			}
			if closed {
				a.importKeyList = nil
			}
			return a, cmd
		}

		if a.importHostList != nil {
			closed, proceed, cmd := a.importHostList.Update(msg)
			if proceed {
				keyItems := buildKeyItems(a.db, a.importHostList.allKeys)
				if a.importHostList.exportMode {
					keyItems = buildExportKeyItems(a.importHostList.items, a.importHostList.allKeys)
				} else if a.importHostList.sshSource {
					keyItems = buildSSHKeyItems(a.db, a.importHostList.allKeys)
				}
				if !a.importHostList.exportMode {
					keyItems = lockRequiredKeys(a.importHostList.items, keyItems)
				}
				a.importKeyList = newImportKeyList(keyItems, a.importHostList.items)
				a.importKeyList.exportMode = a.importHostList.exportMode
				a.importKeyList.setPageSize(a.height)
				return a, cmd
			}
			if closed {
				a.importHostList = nil
				a.importSourceMenu = newImportSourceMenu()
			}
			return a, cmd
		}

		if a.importSourceMenu != nil {
			closed, cmd := a.importSourceMenu.Update(msg)
			if closed {
				a.importSourceMenu = nil
			}
			return a, cmd
		}

		// Snippet picker overlay intercepts all keys when active
		if a.snippetPicker != nil {
			return a.handleSnippetPickerKey(msg)
		}

		if a.upgradePrompt != nil {
			next, uc := a.handleUpgradePromptKey(msg)
			return next, uc
		}

		// ESC menu overlay intercepts all keys when active
		if a.escMenu != nil {
			closed, cmd := a.escMenu.Update(msg)
			if closed {
				a.escMenu = nil
			}
			return a, cmd
		}

		if a.helpOverlay {
			return a.handleHelpOverlayKey(msg)
		}

		// Touch activity for auto-lock
		if a.viewState == MainView {
			a.masterKey.Touch()
		}

		if a.viewState == LoginView {
			break
		}

		if a.allowsListNavigation() {
			switch msg.String() {
			case "tab":
				return a.switchListView(1)
			case "shift+tab":
				return a.switchListView(-1)
			}
		}

		switch {
		// ? opens full-help overlay (non-SSH); SSH keeps ? for the remote shell.
		case key.Matches(msg, a.keyMap.Help):
			if a.activeTabIsSSH() {
				break
			}
			if fullHelpHasAnyBinding(a.contextualHelpKeyMap()) {
				a.helpOverlay = true
				return a, nil
			}
			return a, nil
		// Tab switching first so Ctrl+Tab / Ctrl+Shift+Tab always switch tabs (never leak to SSH PTY).
		case matchAppNextTab(msg, a.keyMap):
			if len(a.tabs) > 1 {
				a.activeTab = (a.activeTab + 1) % len(a.tabs)
				a.tabBar = a.tabBar.SetActive(a.activeTab)
			}
			var layoutCmd tea.Cmd
			a, layoutCmd = layoutTabModels(a)
			var refreshCmd tea.Cmd
			a, refreshCmd = a.refreshActiveHomeConnectivity()
			return a, tea.Batch(layoutCmd, refreshCmd)
		case matchAppPrevTab(msg, a.keyMap):
			if len(a.tabs) > 1 {
				a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
				a.tabBar = a.tabBar.SetActive(a.activeTab)
			}
			var layoutCmd tea.Cmd
			a, layoutCmd = layoutTabModels(a)
			var refreshCmd tea.Cmd
			a, refreshCmd = a.refreshActiveHomeConnectivity()
			return a, tea.Batch(layoutCmd, refreshCmd)
		case key.Matches(msg, a.keyMap.TabPageLeft):
			a.tabBar = a.tabBar.ScrollLeft()
			return a, nil
		case key.Matches(msg, a.keyMap.TabPageRight):
			a.tabBar = a.tabBar.ScrollRight()
			return a, nil
		case matchCtrlShiftAnyOf(msg, a.keyMap.LocalTerminal) || key.Matches(msg, a.keyMap.LocalTerminal):
			return a.openLocalTerminal()
		case matchCtrlShiftAnyOf(msg, a.keyMap.RenameTab) || key.Matches(msg, a.keyMap.RenameTab):
			return a.openActiveTabRenamePrompt()
		case matchCtrlShiftAnyOf(msg, a.keyMap.QuitApp) || key.Matches(msg, a.keyMap.QuitApp):
			return a.quitWithCheck()
		case key.Matches(msg, a.keyMap.Quit):
			if a.activeTabIsSSH() {
				break
			}
			return a.quitWithCheck()
		case key.Matches(msg, a.keyMap.NewTab):
			if a.activeTabIsSSH() {
				break
			}
			return a.openKeysTab()
		case key.Matches(msg, a.keyMap.ForwardTab):
			if a.activeTabIsSSH() || a.activeTabIsEditor() {
				break
			}
			return a.openForwardTab()
		case key.Matches(msg, a.keyMap.SnippetsTab):
			if a.activeTabIsSSH() || a.activeTabIsEditor() {
				break
			}
			return a.openSnippetsTab()
		case matchCtrlShiftAnyOf(msg, a.keyMap.CloseTabSafe) || key.Matches(msg, a.keyMap.CloseTabSafe):
			return a.closeCurrentTabIfAllowed()
		case key.Matches(msg, a.keyMap.CloseTab):
			if a.activeTabIsSSH() {
				break
			}
			return a.closeCurrentTabIfAllowed()
		case matchCtrlShiftAnyOf(msg, a.keyMap.LockApp) || key.Matches(msg, a.keyMap.LockApp):
			return a.lockSession()
		case key.Matches(msg, a.keyMap.Lock):
			if a.activeTabIsSSH() {
				break
			}
			return a.lockSession()
		case viewkeys.MatchKey(msg, a.kbConfig.SnippetPicker):
			// SSH tab handles this in sshview; elsewhere open snippet picker.
			if a.activeTabIsSSH() {
				break
			}
			return a, func() tea.Msg { return types.SnippetPickerRequestMsg{} }
		case key.Matches(msg, a.keyMap.CommandPalette):
			a.commandPalette = newCommandPaletteFromDB(a.db, a.width)
			return a, a.commandPalette.input.Focus()
		case key.Matches(msg, a.keyMap.AIOverlay):
			return a.openAIOverlay()
		case matchCtrlShiftAnyOf(msg, a.keyMap.PasteImageURL) || key.Matches(msg, a.keyMap.PasteImageURL):
			if !a.activeTabIsSSH() {
				break
			}
			return a.startImageURLPaste(nil, true)
		}

		// Alt+1..9 jumps to tab by number
		if idx, ok := matchAltNumber(msg); ok && idx < len(a.tabs) {
			a.activeTab = idx
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			var layoutCmd tea.Cmd
			a, layoutCmd = layoutTabModels(a)
			var refreshCmd tea.Cmd
			a, refreshCmd = a.refreshActiveHomeConnectivity()
			return a, tea.Batch(layoutCmd, refreshCmd)
		}

		// Alt+s cycles through SSH tabs, Alt+f cycles through SFTP tabs
		if k := msg.Key(); k.Mod.Contains(tea.ModAlt) && !k.Mod.Contains(tea.ModShift) {
			var targetType TabType
			switch k.Code {
			case 's':
				targetType = SSHTab
			case 'f':
				targetType = SFTPTab
			}
			if targetType != "" {
				if idx := a.nextTabOfType(targetType); idx >= 0 {
					a.activeTab = idx
					a.tabBar = a.tabBar.SetActive(a.activeTab)
					var layoutCmd tea.Cmd
					a, layoutCmd = layoutTabModels(a)
					return a, layoutCmd
				}
			}
		}

	case tea.PasteMsg:
		if a.quickConnect != nil {
			return a.handleQuickConnectPaste(msg)
		}
		if a.commandPalette != nil {
			a.commandPalette.paste(msg)
			return a, nil
		}
		if a.voiceSettingsView != nil {
			a.voiceSettingsView.paste(msg)
			return a, nil
		}
		if a.aiVisible && a.aiView != nil {
			return a, a.updateAIView(msg)
		}
		if a.renamePrompt != nil {
			a.renamePrompt.paste(msg)
			return a, nil
		}
		if a.sharePrompt != nil {
			a.sharePrompt.Paste(msg)
			return a, nil
		}
		if a.batchTag != nil {
			a.batchTag.paste(msg)
			return a, nil
		}
		if a.batchActions != nil {
			a.batchActions.paste(msg)
			return a, nil
		}
		if a.activeTabIsSSH() {
			if a.imageUploadProgressCh == nil {
				return a.startImageURLPaste(msg, false)
			}
		}
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			updated, cmd := a.tabs[a.activeTab].Model.Update(msg)
			a.tabs[a.activeTab].Model = updated
			return a, cmd
		}

	case tea.MouseClickMsg:
		if a.viewState == MainView {
			a.masterKey.Touch()

			// Overlay mouse handling -- intercept clicks when any overlay is active
			if a.confirm.IsActive() {
				return a.handleOverlayMouse(msg, a.confirm.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					a.confirm, _ = a.confirm.Update(tea.MouseClickMsg(adjustMouse(msg, lx, ly)))
					if !a.confirm.IsActive() {
						cmd := a.processConfirmResult()
						return a, cmd
					}
					return a, nil
				})
			}
			if a.quickConnect != nil {
				return a.handleOverlayMouse(msg, a.quickConnect.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.quickConnectMouse(lx, ly)
				})
			}
			if a.connError != nil {
				return a.handleOverlayMouse(msg, a.connError.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.connErrorMouse(lx, ly)
				})
			}
			if a.commandPalette != nil {
				return a.handleOverlayMouse(msg, a.commandPalette.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.commandPaletteMouse(lx, ly)
				})
			}
			if a.aiVisible && a.aiView != nil {
				return a.handleOverlayMouse(msg, a.aiView.View().Content, func(lx, ly int) (tea.Model, tea.Cmd) {
					return a, a.updateAIView(adjustMouse(msg, lx, ly))
				})
			}
			if a.renamePrompt != nil {
				return a.handleOverlayMouse(msg, a.renamePrompt.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					if ly == 4 {
						return a, a.renamePrompt.input.Focus()
					}
					return a, nil
				})
			}
			if a.batchTag != nil {
				return a.handleOverlayMouse(msg, a.batchTag.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.batchTagMouse(lx, ly)
				})
			}
			if a.batchActions != nil {
				return a.handleOverlayMouse(msg, a.batchActions.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.batchActionsMouse(lx, ly)
				})
			}
			if a.snippetPicker != nil {
				return a.handleOverlayMouse(msg, a.snippetPicker.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.snippetPickerMouse(lx, ly)
				})
			}
			if a.upgradePrompt != nil && !a.upgradePrompt.Busy {
				return a.handleOverlayMouse(msg, upgradePromptView(a.upgradePrompt), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.upgradePromptMouse(lx, ly)
				})
			}
			if a.escMenu != nil {
				return a.handleOverlayMouse(msg, a.escMenu.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.escMenuMouse(lx, ly)
				})
			}
			if a.voiceSettingsView != nil {
				return a.handleOverlayMouse(msg, a.voiceSettingsView.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.voiceSettingsMouse(lx, ly)
				})
			}
			if a.helpOverlay {
				// Click anywhere dismisses help
				a.helpOverlay = false
				return a, nil
			}

			// Tab bar click
			top := a.MainViewChromeTopLines()
			if msg.Y >= 0 && msg.Y < top-1 && len(a.tabs) > 0 {
				updated, changed := a.tabBar.HandleClick(msg.X)
				a.tabBar = updated
				if changed {
					a.activeTab = a.tabBar.ActiveIndex()
					var layoutCmd tea.Cmd
					a, layoutCmd = layoutTabModels(a)
					var refreshCmd tea.Cmd
					a, refreshCmd = a.refreshActiveHomeConnectivity()
					return a, tea.Batch(layoutCmd, refreshCmd)
				}
				var refreshCmd tea.Cmd
				a, refreshCmd = a.refreshActiveHomeConnectivity()
				if refreshCmd != nil {
					return a, refreshCmd
				}
			}
			if a.width >= 52 && a.activeTab >= 0 && a.activeTab < len(a.tabs) && isListView(a.tabs[a.activeTab].Type) && msg.X < listSidebarWidth+1 {
				localY := msg.Y - top - 4
				if localY >= 0 {
					row := localY / 3
					if row >= 0 && row < len(listViewTypes) {
						return a.openListView(listViewTypes[row])
					}
				}
				return a, nil
			}
		}

	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		// Drag-select in the AI conversation area; hidden overlay keeps the
		// tab-level forwarding below.
		if a.aiVisible && a.aiView != nil {
			return a, a.updateAIView(a.aiOverlayMouse(msg))
		}

	case tea.MouseWheelMsg:
		if a.aiVisible && a.aiView != nil {
			return a, a.updateAIView(msg)
		}
		top := a.MainViewChromeTopLines()
		if a.viewState == MainView && msg.Y >= 0 && msg.Y < top-1 && len(a.tabs) > 0 {
			switch msg.Button {
			case tea.MouseWheelLeft, tea.MouseWheelUp:
				a.tabBar = a.tabBar.ScrollLeft()
			case tea.MouseWheelRight, tea.MouseWheelDown:
				a.tabBar = a.tabBar.ScrollRight()
			}
			return a, nil
		}

	case types.SwitchTabMsg:
		if msg.Index >= 0 && msg.Index < len(a.tabs) {
			a.activeTab = msg.Index
			a.tabBar = a.tabBar.SetActive(a.activeTab)
		}
		var layoutCmd tea.Cmd
		a, layoutCmd = layoutTabModels(a)
		var refreshCmd tea.Cmd
		a, refreshCmd = a.refreshActiveHomeConnectivity()
		return a, tea.Batch(layoutCmd, refreshCmd)

	case types.NewTabMsg:
		return a.handleNewTabMsg(msg)

	case types.CloseTabMsg:
		idx := msg.Index
		if idx == -1 {
			idx = a.activeTab
		}
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 && !isListView(a.tabs[idx].Type) {
			return a.closeTabAt(idx)
		}
		return a, nil

	case types.ForwardRuleStartMsg:
		if a.ruleForwardRunning(msg.RuleID) {
			return a, nil
		}
		var toastCmd tea.Cmd
		a.toast, toastCmd = a.toast.Show("Starting forward…", components.ToastInfo, 8*time.Second)
		a2, dialCmd := a.handleForwardRuleStart(msg.RuleID)
		return a2, tea.Batch(toastCmd, dialCmd)

	case types.ForwardRuleStopMsg:
		return a.handleForwardRuleStop(msg.RuleID)

	case forwardRuleAttachMsg:
		return a.attachForward(msg)

	case types.ForwardRuleResultMsg:
		var tc tea.Cmd
		if msg.Err != nil {
			a.toast, tc = a.toast.Show(fmt.Sprintf("Port forward: %v", msg.Err), components.ToastError, 5*time.Second)
		}
		a, bcmd := a.broadcastForwardResult(msg)
		if tc != nil {
			return a, tea.Batch(bcmd, tc)
		}
		return a, bcmd

	case types.SSHConnectMsg:
		return a.applySSHConnect(msg)

	case types.SSHReconnectMsg:
		return a.applySSHReconnect(msg)

	case openSSHUITabMsg:
		return a.applyOpenSSHUITab(msg)

	case localTerminalOpenedMsg:
		return a.applyLocalTerminalOpened(msg)

	case tmuxTerminalOpenedMsg:
		return a.applyTmuxTerminalOpened(msg)

	case types.RemotePeerMenuMsg:
		a.remoteMenu = remotemenu.New(msg.Peer, msg.Hosts)
		a.remoteMenu.SetTmuxLoading(true)
		return a, a.loadRemoteTmuxSessions(msg.Peer)

	case types.RemoteTmuxSessionsLoadedMsg:
		if msg.Err != nil {
			if a.remoteMenu != nil && a.remoteMenu.Peer.ID == msg.Peer.ID {
				a.remoteMenu.SetTmuxError(msg.Err.Error())
				return a, nil
			}
			return a, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		if a.remoteMenu != nil && a.remoteMenu.Peer.ID == msg.Peer.ID {
			a.remoteMenu.SetTmuxSessions(msg.Sessions)
		}
		return a, nil

	case types.RemoteTmuxKillRequestMsg:
		kill := types.RemoteTmuxKillMsg{Peer: msg.Peer, SessionID: msg.SessionID}
		a.pendingRemoteTmuxKill = &kill
		a.confirm = components.NewConfirm("Kill tmux session", fmt.Sprintf("Kill tmux session %s on %s?", msg.SessionID, msg.Peer.Name)).Show()
		return a, nil

	case types.RemoteTmuxKillMsg:
		return a, a.killRemoteTmuxSession(msg)

	case types.RemoteTmuxRenameRequestMsg:
		a.renamePrompt = newRemoteTmuxRenamePrompt(msg)
		a.renamePrompt.syncWidth(a.width)
		return a, textinput.Blink

	case types.RemoteTmuxRenameMsg:
		return a.renameRemoteTmuxSession(msg)

	case remoteTmuxRenameAppliedMsg:
		a.renameRemoteTmuxTabs(msg.Peer.ID, msg.OldSessionID, msg.Name)
		return a, a.loadRemoteTmuxSessions(msg.Peer)

	case tmuxRenameAppliedMsg:
		a.renameTmuxTabs(msg.OldName, msg.NewName)
		return a, a.loadTmuxSessions()

	case types.RemoteShellReconnectMsg:
		return a.applyRemoteShellReconnect(msg)

	case types.RemoteShellOpenMsg:
		return a.openRemoteShell(msg)

	case types.RemoteShareMsg:
		a.sharePrompt = shareview.New(msg.Peer, msg.Target, msg.SessionID, msg.Label, a.shareDefaultMaxHours())
		a.sharePrompt.SetWidth(a.width)
		return a, textinput.Blink

	case types.RemoteShareSubmitMsg:
		return a.shareRemoteShell(msg)

	case remoteShareLinkMsg:
		var tc tea.Cmd
		if msg.err != nil {
			a.toast, tc = a.toast.Show(fmt.Sprintf("Share failed: %v", msg.err), components.ToastError, 5*time.Second)
			return a, tc
		}
		a.toast, tc = a.toast.Show(fmt.Sprintf("Link copied (%s): %s (expires %s)", msg.label, msg.url, msg.expiresAt.Local().Format("2006-01-02 15:04")), components.ToastSuccess, 8*time.Second)
		return a, tea.Batch(tc, tea.SetClipboard(msg.url))

	case remoteTerminalOpenedMsg:
		return a.applyRemoteTerminalOpened(msg)

	case types.TmuxMenuMsg:
		a.tmuxMenu = tmuxmenu.New(nil)
		a.tmuxMenu.SetLoading(true)
		return a, a.loadTmuxSessions()

	case types.TmuxSessionsLoadedMsg:
		if msg.Err != nil {
			if a.tmuxMenu != nil {
				a.tmuxMenu.SetError(msg.Err.Error())
				return a, nil
			}
			return a, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		if a.tmuxMenu != nil {
			a.tmuxMenu.SetSessions(msg.Sessions)
		}
		return a, nil

	case types.TmuxOpenMsg:
		return a.openTmux(msg)

	case types.TmuxKillRequestMsg:
		kill := types.TmuxKillMsg{Name: msg.Name}
		a.pendingTmuxKill = &kill
		a.confirm = components.NewConfirm("Kill tmux session", fmt.Sprintf("Kill tmux session %s?", msg.Name)).Show()
		return a, nil

	case types.TmuxKillMsg:
		return a, a.killTmuxSession(msg)

	case types.TmuxRenameRequestMsg:
		a.renamePrompt = newTmuxRenamePrompt(msg)
		a.renamePrompt.syncWidth(a.width)
		return a, textinput.Blink

	case types.TmuxRenameMsg:
		return a.renameTmuxSession(msg)

	case tabRenameMsg:
		return a.renameTab(msg)

	case types.SSHDisconnectMsg:
		return a.applySSHDisconnect(msg)

	case types.SFTPOpenMsg:
		return a.applySFTPOpen(msg)

	case sftpOpenedMsg:
		return a.applySftpOpened(msg)

	case types.HostSavedMsg:
		return a.closeEditorTab(EditorTab)

	case types.HostDeletedMsg:
		database := a.db
		id := msg.ID
		return a, func() tea.Msg {
			if err := db.DeleteHostForSync(database, id); err != nil {
				return types.ErrorMsg{Err: err}
			}
			return types.RefreshListMsg{}
		}

	case types.HostToggleHiddenMsg:
		database := a.db
		hostID := msg.HostID
		return a, func() tea.Msg {
			var host db.Host
			if err := database.First(&host, hostID).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
			tags := strings.Split(host.Tags, ",")
			found := false
			var newTags []string
			for _, t := range tags {
				t = strings.TrimSpace(t)
				if t == "" {
					continue
				}
				if strings.EqualFold(t, "hidden") {
					found = true
					continue
				}
				newTags = append(newTags, t)
			}
			if !found {
				newTags = append(newTags, "hidden")
			}
			host.Tags = strings.Join(newTags, ",")
			if err := database.Save(&host).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
			return types.RefreshListMsg{}
		}

	case types.HostCloneMsg:
		database := a.db
		hostID := msg.HostID
		return a, func() tea.Msg {
			var src db.Host
			if err := database.First(&src, hostID).Error; err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("clone: %w", err)}
			}
			base := src.Alias
			if base == "" {
				base = src.Hostname
			}
			// Find a unique suffix: "name (2)", "name (3)", ...
			alias := base
			for n := 2; ; n++ {
				candidate := fmt.Sprintf("%s (%d)", base, n)
				var count int64
				database.Model(&db.Host{}).Where("alias = ?", candidate).Count(&count)
				if count == 0 {
					alias = candidate
					break
				}
			}
			clone := db.Host{
				Alias:           alias,
				Hostname:        src.Hostname,
				Port:            src.Port,
				Username:        src.Username,
				AuthMethod:      src.AuthMethod,
				Password:        src.Password,
				KeyID:           src.KeyID,
				Passphrase:      src.Passphrase,
				JumpHostID:      src.JumpHostID,
				Tags:            src.Tags,
				Description:     src.Description,
				Group:           src.Group,
				ProxyType:       src.ProxyType,
				ProxyHost:       src.ProxyHost,
				ProxyPort:       src.ProxyPort,
				ProxyUser:       src.ProxyUser,
				ProxyPassword:   src.ProxyPassword,
				ProxyCommand:    src.ProxyCommand,
				GSSAPISource:    src.GSSAPISource,
				GSSAPIKeytab:    src.GSSAPIKeytab,
				KrbPrincipal:    src.KrbPrincipal,
				ForwardAgent:    src.ForwardAgent,
				RemoteCommand:   src.RemoteCommand,
				ExtraSSHOptions: src.ExtraSSHOptions,
			}
			if err := database.Create(&clone).Error; err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("clone: %w", err)}
			}
			return types.RefreshListMsg{}
		}

	case types.MasterKeyUnlockedMsg:
		a.viewState = MainView
		a.statusBar = a.statusBar.SetLocked(false)
		a.noPasswordMode = msg.NoPassword
		if msg.IsSetup {
			db.SetSetting(a.db, "encryption_salt", msg.Salt)
			db.SetSetting(a.db, "encryption_verifier", msg.Verifier)
		}
		if msg.NoPassword {
			db.SetSetting(a.db, "no_password", "true")
		} else {
			db.SetSetting(a.db, "no_password", "false")
		}
		homeModel := home.New(a.db, a.masterKey, BuildHomeKeyConfig(a.kbConfig))
		homeModel.SetSize(listContentWidth(a.width), a.mainContentHeightForType(HomeTab))
		a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: homeModel}}
		a.activeTab = 0
		a.syncTabBar()
		a.scheduleTmuxRestoreAfterUnlock()
		unlockCmds := []tea.Cmd{autoLockTick()}
		if a.width > 0 && a.height > 0 {
			unlockCmds = append(unlockCmds, tea.Sequence(reflowWindow(a), homeModel.Init()))
		} else {
			unlockCmds = append(unlockCmds, homeModel.Init())
		}
		// If CLI direct connect was requested, trigger it after home loads.
		if a.pendingCLIConnect != nil {
			info := a.pendingCLIConnect
			a.pendingCLIConnect = nil
			unlockCmds = append(unlockCmds, func() tea.Msg {
				return types.CLIConnectMsg{
					Hostname: info.Hostname,
					Port:     info.Port,
					Username: info.Username,
				}
			})
		}
		if a.forceUpdateCheck {
			unlockCmds = append(unlockCmds, func() tea.Msg {
				tag, url, err := version.CheckLatestRelease()
				return types.UpdateCheckDoneMsg{Version: tag, URL: url, Err: err}
			})
		} else {
			// Async version check (throttled in DB; ETERM_NO_UPDATE_CHECK / --no-update-check disables).
			unlockCmds = append(unlockCmds, func() tea.Msg {
				disabled := a.noUpdateCheck || os.Getenv("ETERM_NO_UPDATE_CHECK") != ""
				tag, url, err := version.PollLatestRelease(a.db, disabled)
				if err != nil || tag == "" {
					return nil
				}
				return types.UpdateAvailableMsg{Version: tag, URL: url}
			})
		}
		// Start sync tick if enabled
		unlockCmds = append(unlockCmds, syncTickCmd(a.db))
		var aiCmd tea.Cmd
		a, aiCmd = a.ensureAI()
		unlockCmds = append(unlockCmds, aiCmd)
		return a, tea.Batch(unlockCmds...)

	case types.MasterKeyLockedMsg:
		a.viewState = LoginView
		a.statusBar = a.statusBar.SetLocked(true)
		a.aiVisible = false
		if a.aiBridge != nil {
			a.aiBridge.CancelRun()
		}
		return a.stopVoice()

	case types.ErrorMsg:
		appDebugf("ErrorMsg (toast): %v", msg.Err)
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.Err.Error(), components.ToastError, 5*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))

	case types.RemoteDaemonLoadingMsg:
		if msg.Silent {
			return a, nil
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("Loading daemon peers...", components.ToastInfo, 30*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))

	case types.RemoteDaemonLoadedMsg:
		if msg.Err != nil {
			a.aiShared.setPeers(nil)
			if msg.Silent {
				return a, a.forwardToHomeTabs(msg)
			}
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Daemon peers unavailable: "+msg.Err.Error(), components.ToastWarning, 5*time.Second)
			return a, tea.Batch(tc, a.forwardToHomeTabs(msg), reflowWindow(a))
		}
		a.aiShared.setPeers(msg.Peers)
		if !msg.Silent {
			a.toast = a.toast.Dismiss()
		}
		return a, tea.Batch(a.forwardToHomeTabs(msg), reflowWindow(a))

	case types.RemoteDaemonRefreshMsg:
		return a, a.forwardToHomeTabs(msg)

	case types.ConnErrorMsg:
		appDebugf("ConnErrorMsg: %v", msg.Err)
		a = a.stopConnectProgress()
		if retry, ok := msg.Retry.(types.RemoteShellReconnectMsg); ok {
			for i := range a.tabs {
				if sm, ok := a.tabs[i].Model.(*sshview.Model); ok && sm.StreamID() == retry.StreamID {
					sm.SetReconnecting(0, 0)
					break
				}
			}
		}
		a.connError = newConnErrorModel(internalssh.Classify(msg.Err), msg.Target, msg.Retry)
		return a, reflowWindow(a)

	case tmuxRestoreOpenedMsg:
		return a.applyTmuxRestoreOpened(msg)

	case types.QuitRequestMsg:
		return a.quitWithCheck()

	case types.EscMenuRequestMsg:
		a.escMenu = newEscMenu()
		return a, nil

	case aiview.CloseMsg:
		a.aiVisible = false
		return a, nil

	case aiToolRequestMsg:
		var toolCmd tea.Cmd
		a, toolCmd = a.handleAIToolRequest(msg.req)
		return a, tea.Batch(toolCmd, waitAIToolRequest(a.aiToolCh))

	case aiToolSendKeysDoneMsg:
		return a.handleAIToolSendKeysDone(msg)

	case aiToolRenameDoneMsg:
		if msg.err == nil {
			a.renameRemoteTmuxTabs(msg.peer.ID, msg.req.arg, msg.req.arg2)
		}
		msg.req.respond(aiToolResult{err: msg.err})
		return a, nil

	case voiceEventMsg:
		return a.handleVoiceEvent(msg)

	case voiceEngineClosedMsg:
		return a, nil

	case voiceProgressMsg:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Downloading voice helper %.0f%%", msg.pct), components.ToastInfo, 30*time.Second)
		return a, tea.Batch(tc, waitVoiceProgress(a.voiceProgressCh))

	case voiceStartedMsg:
		a.voiceBusy = false
		if !a.voiceRec && !a.voiceTest {
			// Toggled off while the (downloading) start was in flight.
			a.voiceBusy = true
			return a, voiceStopCmd(a.voiceEngine)
		}
		if a.voiceTest {
			return a, nil
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("Voice recording", components.ToastSuccess, 2*time.Second)
		return a, tc

	case voiceStoppedMsg:
		a.voiceBusy = false
		if a.voiceRec {
			// Toggled back on while stopping.
			a.voiceBusy = true
			a.voiceStartedAt = time.Now()
			a.voiceTickSeq++
			return a, tea.Batch(voiceStartCmd(a.voiceEngine), voiceTick(a.voiceTickSeq))
		}
		return a, nil

	case voiceStartFailedMsg:
		a.voiceBusy = false
		a.voiceRec = false
		a.voicePartial = ""
		if a.aiView != nil {
			a.aiView.SetVoiceActive(false)
		}
		if a.voiceTest {
			a.voiceTest = false
			a.voiceTestSeq++
			a.voiceSwallowFinal = false
			if a.voiceSettingsView != nil {
				a.voiceSettingsView.testError(msg.err.Error())
			}
			return a, nil
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("Voice: "+msg.err.Error(), components.ToastError, 6*time.Second)
		return a, tc

	case voiceTickMsg:
		if a.voiceRec && msg.seq == a.voiceTickSeq {
			return a, voiceTick(msg.seq)
		}
		return a, nil

	case voiceDownloadRequestMsg:
		return a.handleVoiceDownloadRequest(msg)

	case voiceDownloadMsg:
		return a.handleVoiceDownload(msg)

	case voiceTestRequestMsg:
		return a.handleVoiceTestRequest(msg)

	case voiceTestTimeoutMsg:
		if !a.voiceTest || msg.seq != a.voiceTestSeq {
			return a, nil
		}
		return a.endVoiceTest()

	case voiceHelperUpdateCheckRequestMsg:
		return a, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			tag, err := latestHelperVersionFn(ctx)
			return voiceHelperUpdateCheckMsg{tag: tag, err: err}
		}

	case voiceHelperUpdateCheckMsg:
		if a.voiceSettingsView != nil {
			a.voiceSettingsView.updateCheckDone(msg.tag, msg.err)
		}
		return a, nil

	case openVoiceSettingsMsg:
		a = a.ensureVoiceCfg()
		a.voiceSettingsView = newVoiceSettingsModel(a.db, a.masterKey, a.voiceCfg)
		return a, nil

	case voiceSettingsChangedMsg:
		a.voiceCfg = msg.cfg
		if a.voiceEngine == nil {
			return a, nil
		}
		if msg.keepEngine {
			_ = a.voiceEngine.SetVAD(msg.cfg.vadParams())
			if msg.cfg.Engine == voiceEngineLocal {
				dir, kind := localModelTarget(msg.cfg, voice.ModelsRoot())
				_ = a.voiceEngine.SetModel(dir, kind)
			}
			return a, nil
		}
		eng := a.voiceEngine
		a.voiceEngine = nil
		a.voiceRec = false
		a.voiceBusy = false
		a.voicePartial = ""
		if a.aiView != nil {
			a.aiView.SetVoiceActive(false)
		}
		return a, func() tea.Msg {
			_ = eng.Close()
			return nil
		}

	case types.OpenImportSourceMenuMsg:
		a.importSourceMenu = newImportSourceMenu()
		return a, nil

	case openExportConfigMsg:
		model, err := newExportLists(a.db)
		if err != nil {
			return a, func() tea.Msg { return types.ErrorMsg{Err: err} }
		}
		model.setPageSize(a.height)
		a.importHostList = model
		return a, nil

	case termiusLoadMsg:
		return a, loadTermiusData()

	case sshConfigLoadMsg:
		return a, loadSSHConfigData()

	case termiusExportResultMsg:
		if msg.err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(fmt.Sprintf("Import source error: %v", msg.err), components.ToastError, 5*time.Second)
			a.importSourceMenu = nil
			return a, tc
		}
		hostItems := buildHostItems(a.db, msg.hosts)
		if msg.sshSource {
			hostItems = buildSSHHostItems(a.db, msg.sshParsed, msg.hosts)
		}
		hl := newImportHostList(hostItems)
		hl.setPageSize(a.height)
		hl.allKeys = msg.keys
		hl.sshSource = msg.sshSource
		a.importHostList = hl
		a.importSourceMenu = nil
		return a, nil

	case termiusImportResultMsg:
		a.importKeyList = nil
		a.importHostList = nil
		var tc tea.Cmd
		if msg.err != nil {
			a.toast, tc = a.toast.Show(fmt.Sprintf("Import error: %v", msg.err), components.ToastError, 5*time.Second)
		} else {
			a.toast, tc = a.toast.Show(fmt.Sprintf("Import complete: %d imported, %d skipped", msg.imported, msg.skipped), components.ToastSuccess, 3*time.Second)
		}
		return a, tea.Batch(tc, func() tea.Msg { return types.RefreshListMsg{} })

	case types.OpenSettingsMsg:
		return a.openSettingsTab()

	case types.OpenSyncMsg:
		return a.openSyncTab()

	case types.SyncResultMsg:
		a.syncing = false
		if msg.Err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(fmt.Sprintf("Sync error: %v", msg.Err), components.ToastError, 5*time.Second)
			return a, tea.Batch(tc, syncTickCmd(a.db))
		}
		var tc tea.Cmd
		tmsg := fmt.Sprintf("Sync: %d pulled, %d pushed", msg.Pulled, msg.Pushed)
		if msg.Failed > 0 {
			tmsg += fmt.Sprintf(", %d failed", msg.Failed)
		}
		tt := components.ToastInfo
		if msg.Failed > 0 {
			tt = components.ToastWarning
		}
		a.toast, tc = a.toast.Show(tmsg, tt, 3*time.Second)
		return a, tea.Batch(tc, syncTickCmd(a.db), func() tea.Msg { return types.RefreshListMsg{} })

	case types.SyncTestResultMsg:
		// Forward to active sync tab if any
		for _, tab := range a.tabs {
			if tab.Type == SyncTab {
				var cmd tea.Cmd
				tab.Model, cmd = tab.Model.Update(msg)
				return a, cmd
			}
		}
		return a, nil

	case types.SyncStartMsg:
		if a.syncing {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Sync already running", components.ToastInfo, 2*time.Second)
			return a, tc
		}
		cmd, inFlight := a.prepareSync(true)
		if cmd == nil {
			return a, nil
		}
		a.syncing = inFlight
		return a, cmd

	case types.SyncTickMsg:
		if a.syncing {
			return a, nil // skip, previous sync still running
		}
		cmd, inFlight := a.prepareSync(false)
		if cmd == nil {
			return a, nil
		}
		a.syncing = inFlight
		return a, cmd

	case types.KeyBindingsChangedMsg:
		a.kbConfig = LoadKeyBindingConfig(a.db)
		a.keyMap = BuildKeyMap(a.kbConfig)
		// Propagate to all open tabs
		for i := range a.tabs {
			switch a.tabs[i].Type {
			case HomeTab:
				if m, ok := a.tabs[i].Model.(home.Model); ok {
					a.tabs[i].Model = m.WithUpdatedKeys(BuildHomeKeyConfig(a.kbConfig))
				}
			case SFTPTab:
				if m, ok := a.tabs[i].Model.(sftpview.Model); ok {
					m.SetViewKeys(BuildSFTPKeys(a.kbConfig))
					a.tabs[i].Model = m
				}
			case KeyTab:
				if m, ok := a.tabs[i].Model.(keyview.Model); ok {
					m.SetViewKeys(BuildKeyViewKeys(a.kbConfig))
					a.tabs[i].Model = m
				}
			case ForwardTab:
				if m, ok := a.tabs[i].Model.(fwdview.Model); ok {
					m.SetViewKeys(BuildFwdKeys(a.kbConfig))
					a.tabs[i].Model = m
				}
			case SnippetTab:
				if m, ok := a.tabs[i].Model.(*snippetview.Model); ok {
					m.SetViewKeys(BuildSnippetKeys(a.kbConfig))
				}
			case SSHTab, LocalTab:
				if m, ok := a.tabs[i].Model.(*sshview.Model); ok {
					m.SetViewKeys(BuildSSHKeys(a.kbConfig))
				}
			}
		}
		return a, nil

	case types.RefreshListMsg:
		var refreshCmds []tea.Cmd
		for i := range a.tabs {
			tab := a.tabs[i]
			if tab.Model == nil {
				continue
			}
			switch tab.Type {
			case HomeTab, KeyTab, ForwardTab, SnippetTab, SessionListTab:
				updated, cmd := tab.Model.Update(msg)
				a.tabs[i].Model = updated
				if cmd != nil {
					refreshCmds = append(refreshCmds, cmd)
				}
			}
		}
		return a, tea.Batch(refreshCmds...)

	case types.UpdateAvailableMsg:
		dismissed, _ := db.GetSetting(a.db, version.SettingUpgradeDismissedTag)
		if dismissed == msg.Version {
			return a, nil
		}
		a.upgradePrompt = NewUpgradePrompt(msg.Version, msg.URL)
		return a, nil

	case types.UpdateCheckDoneMsg:
		if msg.Err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Update check failed: "+msg.Err.Error(), components.ToastError, 6*time.Second)
			a.promptDeferredTmuxRestore()
			return a, tc
		}
		if msg.Version == "" {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Already up to date.", components.ToastInfo, 4*time.Second)
			a.promptDeferredTmuxRestore()
			return a, tc
		}
		a.upgradePrompt = NewUpgradePrompt(msg.Version, msg.URL)
		return a, nil

	case types.UpgradeDownloadDoneMsg:
		return a.handleUpgradeDownloadDone(msg)

	case batchTagApplyMsg:
		return a, func() tea.Msg {
			return applyBatchTags(a.db, msg)
		}

	case types.MasterPasswordChangeMsg:
		err := security.RotateMasterPassword(a.db, a.masterKey, []byte(msg.Current), []byte(msg.New), a.noPasswordMode)
		if err != nil {
			return a, func() tea.Msg { return types.ErrorMsg{Err: err} }
		}
		a.noPasswordMode = false
		for i := range a.tabs {
			if a.tabs[i].Type == SettingsTab {
				if sm, ok := a.tabs[i].Model.(*settingsview.Model); ok {
					sm.SetNoPasswordMode(false)
				}
			}
		}
		return a, func() tea.Msg { return types.SuccessMsg{Message: "Master password updated"} }

	case types.ImageUploadProgressMsg:
		pct := 0.0
		if msg.TotalBytes > 0 {
			pct = float64(msg.SentBytes) / float64(msg.TotalBytes) * 100
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Uploading clipboard %.1f%%", pct), components.ToastInfo, 30*time.Second)
		if a.imageUploadProgressCh != nil {
			return a, tea.Batch(tc, waitImageUploadProgressCmd(a.imageUploadProgressCh, msg.StreamID))
		}
		return a, tc

	case types.ImageUploadDoneMsg:
		a.imageUploadProgressCh = nil
		if msg.Err != nil {
			return a, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		if msg.CacheKey != "" && msg.URL != "" && msg.ExpiresAt.After(time.Now()) {
			if a.imageURLCache == nil {
				a.imageURLCache = make(map[string]imageURLCacheEntry)
			}
			a.imageURLCache[msg.CacheKey] = imageURLCacheEntry{URL: msg.URL, Filename: msg.Filename, ExpiresAt: msg.ExpiresAt}
		}
		if m := sshViewByStreamID(&a, msg.StreamID); m != nil {
			m.PasteText(markdownBlobLink(msg.Filename, msg.URL) + " ")
		}
		return a, func() tea.Msg { return types.SuccessMsg{Message: "URL pasted"} }

	case types.PasteImageURLMsg:
		return a.startImageURLPaste(nil, true)

	case imagePasteFallbackMsg:
		a.imageUploadProgressCh = nil
		a.toast = a.toast.Dismiss()
		if m := sshViewByStreamID(&a, msg.streamID); m != nil {
			updated, cmd := m.Update(msg.msg)
			for i := range a.tabs {
				if a.tabs[i].Model == m {
					a.tabs[i].Model = updated
					return a, cmd
				}
			}
		}
		return a, nil

	case types.SuccessMsg:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.Message, components.ToastSuccess, 3*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))

	case types.AutoLockTickMsg:
		if a.noPasswordMode || a.viewState != MainView {
			return a, autoLockTick()
		}
		if a.masterKey.CheckTimeout() {
			a.viewState = LoginView
			a.statusBar = a.statusBar.SetLocked(true)
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("Session locked (timeout)", components.ToastWarning, 3*time.Second)
			return a, tea.Batch(tc, func() tea.Msg { return types.MasterKeyLockedMsg{} })
		}
		return a, autoLockTick()

	case types.HostDeleteRequestMsg:
		a.pendingDeleteID = msg.ID
		title := "Delete Host"
		message := fmt.Sprintf("Delete host %q? This cannot be undone.", msg.Alias)
		a.confirm = components.NewConfirm(title, message).Show()
		return a, nil

	case types.SnippetDeleteRequestMsg:
		a.pendingSnippetDeleteID = msg.ID
		title := "Delete Snippet"
		message := fmt.Sprintf("Delete snippet %q?", msg.Name)
		a.confirm = components.NewConfirm(title, message).Show()
		return a, nil

	case types.SnippetDeletedMsg:
		return a, func() tea.Msg { return types.RefreshListMsg{} }

	case types.ForwardRuleDeleteRequestMsg:
		a.pendingFwdDeleteID = msg.ID
		title := "Delete Forward Rule"
		message := fmt.Sprintf("Delete %q?", msg.Desc)
		a.confirm = components.NewConfirm(title, message).Show()
		return a, nil

	case types.ForwardRuleDeletedMsg:
		return a, func() tea.Msg { return types.RefreshListMsg{} }

	case types.ForwardRuleSavedMsg:
		return a.closeEditorTab(FwdEditorTab)

	case types.SnippetSavedMsg:
		return a.closeEditorTab(SnippetEditorTab)

	case types.FingerprintConfirmMsg:
		a = a.stopConnectProgress()
		a.pendingFingerprint = &msg
		a.confirm = components.NewConfirm(fingerprintConfirmTitle(msg), fingerprintConfirmBody(msg)).Show()
		return a, nil

	case types.FingerprintAcceptedMsg:
		switch msg.ConnType {
		case "ssh":
			return a, func() tea.Msg { return types.SSHConnectMsg{HostID: msg.HostID} }
		case "sftp":
			return a, func() tea.Msg { return types.SFTPOpenMsg{HostID: msg.HostID} }
		case "reconnect":
			return a, func() tea.Msg { return types.SSHReconnectMsg{HostID: msg.HostID, StreamID: msg.StreamID} }
		case "quick":
			if a.pendingQuickConnect != nil {
				next := *a.pendingQuickConnect
				a.pendingQuickConnect = nil
				return a.handleQuickConnect(next)
			}
			return a, nil
		case "forward":
			if msg.ForwardRuleID != 0 {
				rid := msg.ForwardRuleID
				return a, func() tea.Msg { return types.ForwardRuleStartMsg{RuleID: rid} }
			}
		}
		return a, nil

	case types.QuickConnectRequestMsg:
		a.quickConnect = newQuickConnectModel()
		if a.width > 0 {
			a.quickConnect.syncInputWidth(a.width)
		}
		return a, a.quickConnect.input.Focus()

	case types.QuickConnectMsg:
		a.quickConnect = nil
		return a.handleQuickConnect(msg)

	case types.CLIConnectMsg:
		return a.handleCLIConnect(msg)

	case types.OpenSessionHistoryMsg:
		return a.openSessionHistoryTab(msg.HostID)

	case types.OpenSessionReplayMsg:
		return a.openSessionReplayTab(msg.HistoryID, msg.Title)

	case types.BatchTagRequestMsg:
		if len(msg.HostIDs) == 0 {
			return a, nil
		}
		a.batchTag = newBatchTagModel(msg.HostIDs)
		if a.width > 0 {
			a.batchTag.syncWidth(a.width)
		}
		return a, a.batchTag.input.Focus()

	case types.BatchActionsRequestMsg:
		if len(msg.HostIDs) == 0 {
			return a, nil
		}
		a.batchActions = newBatchActionsModel(msg.HostIDs)
		if a.width > 0 {
			a.batchActions.syncWidth(a.width)
		}
		return a, nil

	case types.BatchActionSelectedMsg:
		switch msg.Action {
		case "open":
			if len(msg.HostIDs) > 8 {
				a.pendingBatchOpenHosts = append([]uint(nil), msg.HostIDs...)
				a.confirm = components.NewConfirm("Open Many Sessions", fmt.Sprintf("Open %d SSH tabs?", len(msg.HostIDs))).Show()
				return a, nil
			}
			return a.runBatchOpenSessions(msg.HostIDs)
		case "snippet":
			a.pendingBatchSnippetHostIDs = append([]uint(nil), msg.HostIDs...)
			a.snippetPicker = newSnippetPickerModel(a.db)
			return a, nil
		}
		return a, nil

	case types.BatchCommandSubmitMsg:
		return a.openBatchResultTab(msg.HostIDs, msg.Command)

	case types.ExportConfigResultMsg:
		a.importKeyList = nil
		a.importHostList = nil
		if msg.Err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(fmt.Sprintf("Export failed: %v", msg.Err), components.ToastError, 5*time.Second)
			return a, tea.Batch(tc, reflowWindow(a))
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("Exported to %s", msg.Path), components.ToastSuccess, 3*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))

	case types.SnippetPickerRequestMsg:
		database := a.db
		a.snippetPicker = newSnippetPickerModel(database)
		return a, nil

	case types.SnippetSelectedMsg:
		a.snippetPicker = nil
		if len(a.pendingBatchSnippetHostIDs) > 0 {
			hostIDs := append([]uint(nil), a.pendingBatchSnippetHostIDs...)
			a.pendingBatchSnippetHostIDs = nil
			return a.applyBatchSnippet(hostIDs, msg.Command)
		}
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			if m, ok := a.tabs[a.activeTab].Model.(*sshview.Model); ok {
				m.PasteCommand(msg.Command)
			}
		}
		return a, nil
	}

	switch a.viewState {
	case LoginView:
		if a.loginModel != nil {
			updated, cmd := a.loginModel.Update(msg)
			a.loginModel = updated
			cmds = append(cmds, cmd)
		}
	case MainView:
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			tab := a.tabs[a.activeTab]
			if tab.Model != nil {
				fwd := appAdjustMouseForTabContent(a, msg)
				if fwd == nil {
					break
				}
				updated, cmd := tab.Model.Update(fwd)
				a.tabs[a.activeTab].Model = updated
				cmds = append(cmds, cmd)
			}
		}
	}

	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Update(msg)
	cmds = append(cmds, toastCmd)

	var confirmCmd tea.Cmd
	a.confirm, confirmCmd = a.confirm.Update(msg)
	cmds = append(cmds, confirmCmd)

	return a, tea.Sequence(cmds...)
}
