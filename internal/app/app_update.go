package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/batchresultview"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"github.com/huangzheng2016/eTerm/internal/ui/home"
	"github.com/huangzheng2016/eTerm/internal/ui/keyview"
	"github.com/huangzheng2016/eTerm/internal/ui/settingsview"
	"github.com/huangzheng2016/eTerm/internal/ui/sftpview"
	"github.com/huangzheng2016/eTerm/internal/ui/snippetview"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/version"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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
		for i := range a.tabs {
			if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[i].Model = updated
				return a, cmd
			}
		}
		return a, nil

	case sshview.StreamDoneMsg:
		for i := range a.tabs {
			if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				updated, cmd := m.Update(msg)
				a.tabs[i].Model = updated
				return a, cmd
			}
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
		a.pendingQuickConnect = &msg.info
		a.pendingFingerprint = &msg.confirmInfo
		fp := msg.confirmInfo
		a.confirm = components.NewConfirm(fingerprintConfirmTitle(fp), fingerprintConfirmBody(fp)).Show()
		return a, nil

	case tea.KeyPressMsg:
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

		if a.batchTag != nil {
			return a.handleBatchTagKey(msg)
		}

		if a.batchActions != nil {
			return a.handleBatchActionsKey(msg)
		}

		if a.importStratMenu != nil {
			closed, cmd := a.importStratMenu.Update(msg)
			if closed {
				a.importStratMenu = nil
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

		if a.confirm.IsActive() {
			wasActive := true
			var cmd tea.Cmd
			a.confirm, cmd = a.confirm.Update(msg)
			cmds = append(cmds, cmd)
			// Check if confirm just closed — process pending action
			if wasActive && !a.confirm.IsActive() {
				actionCmd := a.processConfirmResult()
				if actionCmd != nil {
					cmds = append(cmds, actionCmd)
				}
			}
			return a, tea.Batch(cmds...)
		}

		// Touch activity for auto-lock
		if a.viewState == MainView {
			a.masterKey.Touch()
		}

		if a.viewState == LoginView {
			break
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
			return a, layoutCmd
		case matchAppPrevTab(msg, a.keyMap):
			if len(a.tabs) > 1 {
				a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
				a.tabBar = a.tabBar.SetActive(a.activeTab)
			}
			var layoutCmd tea.Cmd
			a, layoutCmd = layoutTabModels(a)
			return a, layoutCmd
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
		}

		// Alt+1..9 jumps to tab by number
		if idx, ok := matchAltNumber(msg); ok && idx < len(a.tabs) {
			a.activeTab = idx
			a.tabBar = a.tabBar.SetActive(a.activeTab)
			var layoutCmd tea.Cmd
			a, layoutCmd = layoutTabModels(a)
			return a, layoutCmd
		}

		// Alt+s cycles through SSH tabs, Alt+f cycles through SFTP tabs
		if k := msg.Key(); k.Mod.Contains(tea.ModAlt) {
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
			if a.importStratMenu != nil {
				return a.handleOverlayMouse(msg, a.importStratMenu.View(), func(lx, ly int) (tea.Model, tea.Cmd) {
					return a.importStratMenuMouse(lx, ly)
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
			if a.helpOverlay {
				// Click anywhere dismisses help
				a.helpOverlay = false
				return a, nil
			}

			// Tab bar click
			top := a.MainViewChromeTopLines()
			if msg.Y >= 0 && msg.Y < top-1 && len(a.tabs) > 0 {
				a.syncTabBar()
				updated, changed := a.tabBar.HandleClick(msg.X)
				a.tabBar = updated
				if changed {
					a.activeTab = a.tabBar.ActiveIndex()
					var layoutCmd tea.Cmd
					a, layoutCmd = layoutTabModels(a)
					return a, layoutCmd
				}
			}
		}

	case tea.MouseWheelMsg:
		top := a.MainViewChromeTopLines()
		if a.viewState == MainView && msg.Y >= 0 && msg.Y < top-1 && len(a.tabs) > 0 {
			a.syncTabBar()
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
		return a, layoutCmd

	case types.NewTabMsg:
		return a.handleNewTabMsg(msg)

	case types.CloseTabMsg:
		idx := msg.Index
		if idx == -1 {
			idx = a.activeTab
		}
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 {
			if m, ok := a.tabs[idx].Model.(*sshview.Model); ok {
				finalizeSSHSession(a.db, m)
				_ = m.Close()
			}
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if a.activeTab >= len(a.tabs) {
				a.activeTab = len(a.tabs) - 1
			}
			a.syncTabBar()
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

	case types.SSHDisconnectMsg:
		return a.applySSHDisconnect(msg)

	case types.SFTPOpenMsg:
		return a.applySFTPOpen(msg)

	case sftpOpenedMsg:
		return a.applySftpOpened(msg)

	case types.HostSavedMsg:
		// Close the editor tab and refresh the home list
		idx := a.activeTab
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 {
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if a.activeTab >= len(a.tabs) {
				a.activeTab = len(a.tabs) - 1
			}
			a.syncTabBar()
		}
		return a, func() tea.Msg { return types.RefreshListMsg{} }

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
		homeModel.SetSize(a.width, a.mainContentHeightForType(HomeTab))
		a.tabs = []Tab{{Type: HomeTab, Title: "List", Model: homeModel}}
		a.activeTab = 0
		a.syncTabBar()
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
		// Async version check (throttled in DB; ETERM_NO_UPDATE_CHECK / --no-update-check disables).
		unlockCmds = append(unlockCmds, func() tea.Msg {
			disabled := a.noUpdateCheck || os.Getenv("ETERM_NO_UPDATE_CHECK") != ""
			tag, url, err := version.PollLatestRelease(a.db, disabled)
			if err != nil || tag == "" {
				return nil
			}
			return types.UpdateAvailableMsg{Version: tag, URL: url}
		})
		// Start sync tick if enabled
		unlockCmds = append(unlockCmds, syncTickCmd(a.db))
		return a, tea.Batch(unlockCmds...)

	case types.MasterKeyLockedMsg:
		a.viewState = LoginView
		a.statusBar = a.statusBar.SetLocked(true)
		return a, nil

	case types.ErrorMsg:
		appDebugf("ErrorMsg (toast): %v", msg.Err)
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.Err.Error(), components.ToastError, 5*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))

	case types.ConnErrorMsg:
		appDebugf("ConnErrorMsg: %v", msg.Err)
		a.toast = a.toast.Dismiss()
		a.connError = newConnErrorModel(internalssh.Classify(msg.Err), msg.Target, msg.Retry)
		return a, reflowWindow(a)

	case types.QuitRequestMsg:
		return a.quitWithCheck()

	case types.EscMenuRequestMsg:
		a.escMenu = newEscMenu()
		return a, nil

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
			return a, nil
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
			case SSHTab:
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
			case HomeTab, KeyTab, ForwardTab, SnippetTab:
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
		// Close the editor tab and refresh
		idx := a.activeTab
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 {
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if a.activeTab >= len(a.tabs) {
				a.activeTab = len(a.tabs) - 1
			}
			a.syncTabBar()
		}
		return a, func() tea.Msg { return types.RefreshListMsg{} }

	case types.SnippetSavedMsg:
		idx := a.activeTab
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 {
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if a.activeTab >= len(a.tabs) {
				a.activeTab = len(a.tabs) - 1
			}
			a.syncTabBar()
		}
		return a, func() tea.Msg { return types.RefreshListMsg{} }

	case types.FingerprintConfirmMsg:
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

	case types.ImportSSHConfigPreviewMsg:
		return a, func() tea.Msg {
			parsed, err := sshconfig.ParseSSHConfig(sshConfigPath())
			if err != nil {
				return types.ErrorMsg{Err: err}
			}
			preview := buildSSHConfigImportPreview(a.db, parsed)
			return preview
		}

	case types.ImportSSHConflictCountMsg:
		if msg.Count == 0 {
			return a, func() tea.Msg {
				return importSSHConfig(a.db, "skip")
			}
		}
		a.importStratMenu = newImportStratMenu(types.ImportSSHConfigPreviewResultMsg{Changed: msg.Count})
		return a, nil

	case types.ImportSSHConfigPreviewResultMsg:
		if msg.Err != nil {
			return a, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		if msg.Added == 0 && msg.Changed == 0 && msg.Skipped == 0 {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show("No SSH config hosts found", components.ToastWarning, 3*time.Second)
			return a, tea.Batch(tc, reflowWindow(a))
		}
		if msg.Changed == 0 && msg.Skipped == 0 {
			return a, func() tea.Msg { return importSSHConfig(a.db, "skip") }
		}
		a.importStratMenu = newImportStratMenu(msg)
		return a, nil

	case types.ImportSSHConfigRunMsg:
		a.importStratMenu = nil
		return a, func() tea.Msg {
			return importSSHConfig(a.db, msg.Strategy)
		}

	case types.ImportSSHConfigResultMsg:
		if msg.Err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(fmt.Sprintf("Import failed: %v", msg.Err), components.ToastError, 5*time.Second)
			return a, tea.Batch(tc, reflowWindow(a))
		}
		var tc tea.Cmd
		tmsg := fmt.Sprintf("Imported %d (%d skipped", msg.Imported, msg.Skipped)
		if msg.Overwritten > 0 {
			tmsg += fmt.Sprintf(", %d overwritten", msg.Overwritten)
		}
		tmsg += ")"
		if msg.UnresolvedProxyJumps > 0 {
			tmsg += fmt.Sprintf(" (%d unresolved ProxyJump)", msg.UnresolvedProxyJumps)
		}
		a.toast, tc = a.toast.Show(tmsg, components.ToastSuccess, 3*time.Second)
		return a, tea.Batch(tc, reflowWindow(a), func() tea.Msg { return types.RefreshListMsg{} })

	case types.OpenSessionHistoryMsg:
		return a.openSessionHistoryTab(msg.HostID)

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

	case types.ExportConfigMsg:
		database := a.db
		return a, func() tea.Msg {
			return exportConfig(database)
		}

	case types.ExportConfigResultMsg:
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
