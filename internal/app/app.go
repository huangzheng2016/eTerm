package app

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/sftp"
	internalssh "github.com/eterm/eterm/internal/ssh"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"
	"github.com/eterm/eterm/internal/ui/editor"
	"github.com/eterm/eterm/internal/ui/fwdeditor"
	"github.com/eterm/eterm/internal/ui/fwdview"
	"github.com/eterm/eterm/internal/ui/home"
	"github.com/eterm/eterm/internal/ui/keyview"
	"github.com/eterm/eterm/internal/ui/sftpview"
	"github.com/eterm/eterm/internal/ui/snippeteditor"
	"github.com/eterm/eterm/internal/ui/snippetview"
	"github.com/eterm/eterm/internal/ui/sshview"
	"github.com/eterm/eterm/internal/version"

	bubbleshelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"gorm.io/gorm"
)

type ViewState int

const (
	LoginView ViewState = iota
	MainView
)

type TabType string

const (
	HomeTab   TabType = "home"
	EditorTab TabType = "editor"
	SFTPTab   TabType = "sftp"
	KeyTab     TabType = "key"
	ForwardTab    TabType = "fwd"
	FwdEditorTab  TabType = "fwd-editor"
	SnippetTab       TabType = "snippets"
	SnippetEditorTab TabType = "snippet-editor"
	SSHTab      TabType = "ssh"
)

type Tab struct {
	Type  TabType
	Title string
	Model tea.Model
}

type App struct {
	db         *gorm.DB
	masterKey  *security.MasterKeyManager
	noPasswordMode bool
	viewState  ViewState
	tabs       []Tab
	activeTab  int
	tabBar     components.TabsModel
	statusBar  components.StatusBar
	helpBubble bubbleshelp.Model
	toast      components.ToastModel
	confirm    components.ConfirmModel
	keyMap     KeyMap
	width      int
	height     int
	loginModel tea.Model
	initCmd    tea.Cmd

	// Pending confirm actions
	pendingDeleteID        uint
	pendingSnippetDeleteID uint
	pendingFwdDeleteID     uint
	pendingFingerprint     *types.FingerprintConfirmMsg
	pendingQuit            bool

	// Quick connect overlay
	quickConnect *quickConnectModel

	// Snippet picker overlay
	snippetPicker *snippetPickerModel

	// Full help overlay (? toggles contextual FullHelp in a centered panel)
	helpOverlay bool

	// CLI direct connect (set before login, triggered after unlock)
	pendingCLIConnect *CLIConnectInfo

	// Port-forward tab: shared SSH sessions per host (see forwardrules.go).
	forwardByHost map[uint]*hostForwardState
}

func NewApp(database *gorm.DB, masterKey *security.MasterKeyManager) App {
	tabs := []Tab{}
	tabBar := components.NewTabs([]components.TabItem{})

	return App{
		db:        database,
		masterKey: masterKey,
		viewState: LoginView,
		tabs:      tabs,
		activeTab: 0,
		tabBar:      tabBar,
		statusBar:   components.NewStatusBar(),
		helpBubble:  newAppHelpBubble(),
		toast:       components.NewToast(),
		confirm:   components.NewConfirm("", ""),
		keyMap:    DefaultKeyMap(),
	}
}

// newAppHelpBubble styles only FullHelp (? overlay); status-bar shortcuts use mainViewStatusBarHint.
func newAppHelpBubble() bubbleshelp.Model {
	m := bubbleshelp.New()
	m.FullSeparator = "    "
	m.Styles.FullKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#c6c6c6"))
	m.Styles.FullDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#a8a8a8"))
	m.Styles.FullSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	return m
}

func (a App) SetLoginModel(m tea.Model) App {
	a.loginModel = m
	return a
}

func (a App) SetInitCmd(cmd tea.Cmd) App {
	a.initCmd = cmd
	return a
}

func (a App) Init() tea.Cmd {
	if a.initCmd != nil {
		return a.initCmd
	}
	if a.loginModel != nil {
		return a.loginModel.Init()
	}
	return nil
}

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

	case tea.KeyPressMsg:
		// Quick connect overlay intercepts all keys when active
		if a.quickConnect != nil {
			return a.handleQuickConnectKey(msg)
		}

		// Snippet picker overlay intercepts all keys when active
		if a.snippetPicker != nil {
			return a.handleSnippetPickerKey(msg)
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
		case matchCtrlShift(msg, 'q') || matchCtrlShift(msg, 'c') || key.Matches(msg, a.keyMap.QuitApp):
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
		case matchCtrlShift(msg, 'w') || key.Matches(msg, a.keyMap.CloseTabSafe):
			return a.closeCurrentTabIfAllowed()
		case key.Matches(msg, a.keyMap.CloseTab):
			if a.activeTabIsSSH() {
				break
			}
			return a.closeCurrentTabIfAllowed()
		case matchCtrlShift(msg, 'l') || key.Matches(msg, a.keyMap.LockApp):
			return a.lockSession()
		case key.Matches(msg, a.keyMap.Lock):
			if a.activeTabIsSSH() {
				break
			}
			return a.lockSession()
		case matchCtrlShift(msg, 's'):
			// SSH tab handles this in sshview; elsewhere open snippet picker (same as home help / ssh status hint).
			if a.activeTabIsSSH() {
				break
			}
			return a, func() tea.Msg { return types.SnippetPickerRequestMsg{} }
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
			// Tab bar occupies row 0 only; don't intercept clicks on content below.
			if msg.Y == 0 && len(a.tabs) > 0 {
				a.syncTabBar()
				updated, changed := a.tabBar.HandleClick(msg.X)
				a.tabBar = updated // always update (scroll may have changed)
				if changed {
					a.activeTab = a.tabBar.ActiveIndex()
					var layoutCmd tea.Cmd
					a, layoutCmd = layoutTabModels(a)
					return a, layoutCmd
				}
			}
		}

	case tea.MouseWheelMsg:
		if a.viewState == MainView && msg.Y == 0 && len(a.tabs) > 0 {
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
			km := keyview.New(a.db, a.masterKey)
			if a.width > 0 {
				km.SetSize(a.width, a.mainContentHeightForType(KeyTab))
			}
			tabModel = km
			initCmd = km.Init()
		case ForwardTab:
			fm := fwdview.New(a.db)
			if a.width > 0 {
				fm.SetSize(a.width, a.mainContentHeightForType(ForwardTab))
			}
			tabModel = fm
			initCmd = fm.Init()
		case SnippetTab:
			sm := snippetview.New(a.db)
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

	case types.CloseTabMsg:
		idx := msg.Index
		if idx == -1 {
			idx = a.activeTab
		}
		if idx >= 0 && idx < len(a.tabs) && len(a.tabs) > 1 {
			if m, ok := a.tabs[idx].Model.(*sshview.Model); ok {
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
		database := a.db
		mk := a.masterKey
		hostID := msg.HostID
		var toastCmd tea.Cmd
		a.toast, toastCmd = a.toast.Show("Connecting...", components.ToastInfo, 30*time.Second)
		ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
		appDebugf("SSHConnectMsg hostID=%d pty=%dx%d (from terminal size)", hostID, ptyCols, ptyRows)
		dial := func() tea.Msg {
			var host db.Host
			if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
				appDebugf("SSH connect aborted: load host: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
			}

			// Fingerprint pre-check: probe if unknown
			if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
				algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
				if err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
				}
				return types.FingerprintConfirmMsg{
					HostID: hostID, Hostname: host.Hostname, Port: host.Port,
					Algorithm: algo, Fingerprint: fp, ConnType: "ssh",
				}
			}

			var jumpHost *db.Host
			var jumpKey *db.SSHKey
			if host.JumpHostID != nil {
				var jh db.Host
				if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
					jumpHost = &jh
					if jh.KeyID != nil {
						jumpKey = &jh.Key
					}
				}
			}

			var hostKey *db.SSHKey
			if host.KeyID != nil {
				hostKey = &host.Key
			}

			client, err := internalssh.Connect(internalssh.ConnectConfig{
				Host:      &host,
				Key:       hostKey,
				JumpHost:  jumpHost,
				JumpKey:   jumpKey,
				MasterKey: mk,
				DB:        database,
				FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
					return true
				},
			})
			if err != nil {
				appDebugf("SSH dial failed: %v", err)
				database.Create(&db.ConnectionHistory{
					HostID: hostID, ConnectedAt: time.Now(), Status: "failed",
				})
				return types.ErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
			}
			appDebugf("SSH dial OK, opening session (PTY %dx%d)", ptyCols, ptyRows)

			now := time.Now()
			database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
			history := db.ConnectionHistory{
				HostID: hostID, ConnectedAt: now, Status: "success",
			}
			database.Create(&history)

			is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols)
			if err != nil {
				client.Client.Close()
				for _, c := range client.Closers {
					_ = c.Close()
				}
				appDebugf("NewInteractiveSession failed: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("failed to start shell: %w", err)}
			}
			is.SetClosers(client.Closers)

			// Start port forwards configured for this host
			startPortForwards(database, client.Client, hostID, is)

			alias := hostDisplayName(host)
			return openSSHUITabMsg{is: is, alias: alias, hostID: hostID, historyID: history.ID, replaceTabAt: -1}
		}
		return a, tea.Batch(toastCmd, reflowWindow(a), dial)

	case types.SSHReconnectMsg:
		replaceAt := -1
		for i := range a.tabs {
			sm, ok := a.tabs[i].Model.(*sshview.Model)
			if !ok || sm.StreamID() != msg.StreamID {
				continue
			}
			if sm.HostID() != msg.HostID || msg.HostID == 0 || !sm.Disconnected() {
				return a, nil
			}
			replaceAt = i
			break
		}
		if replaceAt < 0 {
			return a, nil
		}
		database := a.db
		mk := a.masterKey
		hostID := msg.HostID
		idx := replaceAt
		var toastCmd tea.Cmd
		a.toast, toastCmd = a.toast.Show("Reconnecting...", components.ToastInfo, 30*time.Second)
		ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
		appDebugf("SSHReconnectMsg hostID=%d replaceTab=%d", hostID, idx)
		dial := func() tea.Msg {
			var host db.Host
			if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
				appDebugf("SSH reconnect aborted: load host: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
			}

			// Fingerprint pre-check for reconnect
			if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
				algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
				if err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
				}
				return types.FingerprintConfirmMsg{
					HostID: hostID, Hostname: host.Hostname, Port: host.Port,
					Algorithm: algo, Fingerprint: fp, ConnType: "reconnect", StreamID: msg.StreamID,
				}
			}

			var jumpHost *db.Host
			var jumpKey *db.SSHKey
			if host.JumpHostID != nil {
				var jh db.Host
				if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
					jumpHost = &jh
					if jh.KeyID != nil {
						jumpKey = &jh.Key
					}
				}
			}

			var hostKey *db.SSHKey
			if host.KeyID != nil {
				hostKey = &host.Key
			}

			client, err := internalssh.Connect(internalssh.ConnectConfig{
				Host:      &host,
				Key:       hostKey,
				JumpHost:  jumpHost,
				JumpKey:   jumpKey,
				MasterKey: mk,
				DB:        database,
				FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
					return true
				},
			})
			if err != nil {
				appDebugf("SSH reconnect dial failed: %v", err)
				database.Create(&db.ConnectionHistory{
					HostID: hostID, ConnectedAt: time.Now(), Status: "failed",
				})
				return types.ErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
			}

			now := time.Now()
			database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
			history := db.ConnectionHistory{
				HostID: hostID, ConnectedAt: now, Status: "success",
			}
			database.Create(&history)

			is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols)
			if err != nil {
				client.Client.Close()
				for _, c := range client.Closers {
					_ = c.Close()
				}
				appDebugf("NewInteractiveSession failed: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("failed to start shell: %w", err)}
			}
			is.SetClosers(client.Closers)

			// Start port forwards for reconnect
			startPortForwards(database, client.Client, hostID, is)

			alias := hostDisplayName(host)
			return openSSHUITabMsg{is: is, alias: alias, hostID: hostID, historyID: history.ID, replaceTabAt: idx}
		}
		return a, tea.Batch(toastCmd, reflowWindow(a), dial)

	case openSSHUITabMsg:
		appDebugf("openSSHUITabMsg host=%q replaceTabAt=%d", msg.alias, msg.replaceTabAt)
		a.toast = a.toast.Dismiss()
		sv := sshview.New(msg.is, msg.alias, msg.hostID)
		sv.SetHistoryID(msg.historyID)
		if a.width > 0 {
			sv.SetSize(a.width, a.mainContentHeightForType(SSHTab))
		}
		tab := Tab{Type: SSHTab, Title: msg.alias, Model: sv}
		if msg.replaceTabAt >= 0 && msg.replaceTabAt < len(a.tabs) {
			if old, ok := a.tabs[msg.replaceTabAt].Model.(*sshview.Model); ok {
				_ = old.Close()
			}
			a.tabs[msg.replaceTabAt] = tab
			a.activeTab = msg.replaceTabAt
		} else {
			a.tabs = append(a.tabs, tab)
			a.activeTab = len(a.tabs) - 1
		}
		a.syncTabBar()
		return a, tea.Batch(sv.Init(), reflowWindow(a))

	case types.SSHDisconnectMsg:
		idx := -1
		if msg.StreamID != 0 {
			for i := range a.tabs {
				if a.tabs[i].Type != SSHTab {
					continue
				}
				if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
					idx = i
					break
				}
			}
		}
		if idx < 0 && msg.Alias != "" {
			for i := range a.tabs {
				if a.tabs[i].Type == SSHTab && a.tabs[i].Title == msg.Alias {
					idx = i
					break
				}
			}
		}
		if idx >= 0 {
			if m, ok := a.tabs[idx].Model.(*sshview.Model); ok {
				// Record disconnect time
				if hid := m.HistoryID(); hid > 0 {
					now := time.Now()
					a.db.Model(&db.ConnectionHistory{}).Where("id = ?", hid).Update("disconnected_at", &now)
				}
				_ = m.Close()
			}
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if a.activeTab > idx {
				a.activeTab--
			} else if a.activeTab >= len(a.tabs) {
				a.activeTab = len(a.tabs) - 1
			}
			if len(a.tabs) > 0 && a.activeTab < 0 {
				a.activeTab = 0
			}
			a.syncTabBar()
		}
		var tc tea.Cmd
		if msg.Err != nil {
			a.toast, tc = a.toast.Show(fmt.Sprintf("SSH session ended: %v", msg.Err), components.ToastWarning, 3*time.Second)
		} else {
			a.toast, tc = a.toast.Show("SSH session ended", components.ToastInfo, 2*time.Second)
		}
		return a, tea.Batch(tc, reflowWindow(a), func() tea.Msg { return types.RefreshListMsg{} })

	case types.SFTPOpenMsg:
		database := a.db
		mk := a.masterKey
		hostID := msg.HostID
		appDebugf("SFTPOpenMsg hostID=%d", hostID)
		var toastCmd tea.Cmd
		a.toast, toastCmd = a.toast.Show("Opening SFTP...", components.ToastInfo, 60*time.Second)
		sftpAsync := func() tea.Msg {
			var host db.Host
			if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
				appDebugf("SFTP aborted: load host: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
			}

			// Fingerprint pre-check for SFTP
			if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
				algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
				if err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
				}
				return types.FingerprintConfirmMsg{
					HostID: hostID, Hostname: host.Hostname, Port: host.Port,
					Algorithm: algo, Fingerprint: fp, ConnType: "sftp",
				}
			}

			var hostKey *db.SSHKey
			if host.KeyID != nil {
				hostKey = &host.Key
			}

			var jumpHost *db.Host
			var jumpKey *db.SSHKey
			if host.JumpHostID != nil {
				var jh db.Host
				if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
					jumpHost = &jh
					if jh.KeyID != nil {
						jumpKey = &jh.Key
					}
				}
			}

			client, err := internalssh.Connect(internalssh.ConnectConfig{
				Host:      &host,
				Key:       hostKey,
				JumpHost:  jumpHost,
				JumpKey:   jumpKey,
				MasterKey: mk,
				DB:        database,
				FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
					return true
				},
			})
			if err != nil {
				appDebugf("SFTP SSH dial failed: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("SFTP connection failed: %w", err)}
			}
			appDebugf("SFTP SSH dial OK, creating SFTP layer")

			sftpClient, err := sftp.NewClient(client.Client)
			if err != nil {
				client.Client.Close()
				for _, c := range client.Closers {
					_ = c.Close()
				}
				appDebugf("sftp.NewClient failed: %v", err)
				return types.ErrorMsg{Err: fmt.Errorf("SFTP client failed: %w", err)}
			}

			appDebugf("SFTP ready, opening tab for %q", hostDisplayName(host))
			return sftpOpenedMsg{client: sftpClient, hostAlias: hostDisplayName(host)}
		}
		return a, tea.Batch(toastCmd, reflowWindow(a), sftpAsync)

	case sftpOpenedMsg:
		appDebugf("sftpOpenedMsg: new tab SFTP: %s", msg.hostAlias)
		a.toast = a.toast.Dismiss()
		sv := sftpview.New(msg.client, msg.hostAlias)
		if a.width > 0 {
			sv.SetSize(a.width, a.mainContentHeightForType(SFTPTab))
		}
		tab := Tab{Type: SFTPTab, Title: msg.hostAlias, Model: sv}
		a.tabs = append(a.tabs, tab)
		a.activeTab = len(a.tabs) - 1
		a.syncTabBar()
		return a, tea.Batch(sv.Init(), reflowWindow(a))

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
			if err := database.Delete(&db.Host{}, id).Error; err != nil {
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
				Alias:         alias,
				Hostname:      src.Hostname,
				Port:          src.Port,
				Username:      src.Username,
				AuthMethod:    src.AuthMethod,
				Password:      src.Password,
				KeyID:         src.KeyID,
				Passphrase:    src.Passphrase,
				JumpHostID:    src.JumpHostID,
				Tags:          src.Tags,
				Description:   src.Description,
				Group:         src.Group,
				ProxyType:     src.ProxyType,
				ProxyHost:     src.ProxyHost,
				ProxyPort:     src.ProxyPort,
				ProxyUser:     src.ProxyUser,
				ProxyPassword: src.ProxyPassword,
				ProxyCommand:  src.ProxyCommand,
				GSSAPISource:  src.GSSAPISource,
				GSSAPIKeytab:  src.GSSAPIKeytab,
				KrbPrincipal:  src.KrbPrincipal,
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
		homeModel := home.New(a.db, a.masterKey)
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
		// Async version check (non-blocking, failures are silent).
		unlockCmds = append(unlockCmds, func() tea.Msg {
			tag, url, err := version.CheckLatestRelease()
			if err != nil || tag == "" {
				return nil
			}
			return types.UpdateAvailableMsg{Version: tag, URL: url}
		})
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

	case types.QuitRequestMsg:
		return a.quitWithCheck()

	case types.UpdateAvailableMsg:
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(fmt.Sprintf("eTerm %s available", msg.Version), components.ToastInfo, 8*time.Second)
		return a, tc

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
		title := "Unknown Host Key"
		message := fmt.Sprintf(
			"Host: %s:%d\nAlgorithm: %s\nFingerprint:\n  %s\n\nTrust this host?",
			msg.Hostname, msg.Port, msg.Algorithm, msg.Fingerprint,
		)
		a.confirm = components.NewConfirm(title, message).Show()
		return a, nil

	case types.FingerprintAcceptedMsg:
		switch msg.ConnType {
		case "ssh":
			return a, func() tea.Msg { return types.SSHConnectMsg{HostID: msg.HostID} }
		case "sftp":
			return a, func() tea.Msg { return types.SFTPOpenMsg{HostID: msg.HostID} }
		case "reconnect":
			return a, func() tea.Msg { return types.SSHReconnectMsg{HostID: msg.HostID, StreamID: msg.StreamID} }
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

	case types.ImportSSHConfigMsg:
		database := a.db
		mk := a.masterKey
		return a, func() tea.Msg {
			return importSSHConfig(database, mk)
		}

	case types.ImportSSHConfigResultMsg:
		if msg.Err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(fmt.Sprintf("Import failed: %v", msg.Err), components.ToastError, 5*time.Second)
			return a, tea.Batch(tc, reflowWindow(a))
		}
		var tc tea.Cmd
		tmsg := fmt.Sprintf("Imported %d hosts (%d skipped)", msg.Imported, msg.Skipped)
		if msg.UnresolvedProxyJumps > 0 {
			tmsg += fmt.Sprintf(" (%d unresolved ProxyJump)", msg.UnresolvedProxyJumps)
		}
		a.toast, tc = a.toast.Show(tmsg, components.ToastSuccess, 3*time.Second)
		return a, tea.Batch(tc, reflowWindow(a), func() tea.Msg { return types.RefreshListMsg{} })

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

type openSSHUITabMsg struct {
	is           *internalssh.InteractiveSession
	alias        string
	hostID       uint
	historyID    uint
	replaceTabAt int // append when < 0; otherwise replace a.tabs[replaceTabAt]
}

func (a App) activeTabIsSSH() bool {
	if a.viewState != MainView {
		return false
	}
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	return a.tabs[a.activeTab].Type == SSHTab
}

func (a App) activeTabIsEditor() bool {
	if a.viewState != MainView {
		return false
	}
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	return a.tabs[a.activeTab].Type == EditorTab
}

// nextTabOfType returns the index of the next tab of the given type after activeTab (wrapping).
// Returns -1 if no tab of that type exists.
func (a App) nextTabOfType(t TabType) int {
	n := len(a.tabs)
	for i := 1; i <= n; i++ {
		idx := (a.activeTab + i) % n
		if a.tabs[idx].Type == t {
			return idx
		}
	}
	return -1
}

// matchCtrlShift structurally matches Ctrl+Shift+<letter> regardless of how the terminal
// encodes it. Many terminals send different strings for Ctrl+Shift combos; key.Matches
// (string comparison) misses them. This checks the raw Key struct instead.
func matchCtrlShift(msg tea.KeyPressMsg, letter rune) bool {
	k := msg.Key()
	if !k.Mod.Contains(tea.ModCtrl) || !k.Mod.Contains(tea.ModShift) {
		return false
	}
	lower := unicode.ToLower(letter)
	upper := unicode.ToUpper(letter)
	return k.Code == lower || k.Code == upper || k.ShiftedCode == lower || k.ShiftedCode == upper
}

// matchAppNextTab matches next-tab chords even when key.Matches misses (terminal string quirks).
func matchAppNextTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.NextTab) {
		return true
	}
	k := msg.Key()
	// Alt+n as reliable alternative (works in all terminals)
	if k.Mod.Contains(tea.ModAlt) && k.Code == 'n' {
		return true
	}
	if !k.Mod.Contains(tea.ModCtrl) {
		return false
	}
	if k.Code == tea.KeyTab && !k.Mod.Contains(tea.ModShift) {
		return true
	}
	if k.Code == tea.KeyPgDown {
		return true
	}
	// Ctrl+] as alternative next-tab (works in most terminals)
	if k.Code == ']' {
		return true
	}
	// Ctrl+Right
	if k.Code == tea.KeyRight {
		return true
	}
	return false
}

// matchAppPrevTab matches previous-tab chords; Ctrl+Shift+Tab is matched structurally so it
// always switches tabs and is never forwarded to an SSH PTY.
func matchAppPrevTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.PrevTab) {
		return true
	}
	k := msg.Key()
	// Alt+p as reliable alternative (works in all terminals)
	if k.Mod.Contains(tea.ModAlt) && k.Code == 'p' {
		return true
	}
	if !k.Mod.Contains(tea.ModCtrl) {
		return false
	}
	if k.Code == tea.KeyTab && k.Mod.Contains(tea.ModShift) {
		return true
	}
	if k.Code == tea.KeyPgUp {
		return true
	}
	// Ctrl+Left
	if k.Code == tea.KeyLeft {
		return true
	}
	// Ctrl+[ is ESC (0x1b), skip it
	return false
}

// matchAltNumber checks for Alt+1..9 or Ctrl+1..9 to jump to a specific tab.
func matchAltNumber(msg tea.KeyPressMsg) (int, bool) {
	k := msg.Key()
	if k.Code >= '1' && k.Code <= '9' {
		if k.Mod.Contains(tea.ModAlt) || k.Mod.Contains(tea.ModCtrl) {
			return int(k.Code - '1'), true
		}
	}
	// Fallback: parse the string representation for terminals that encode Alt as ESC prefix.
	s := msg.String()
	if len(s) >= 3 {
		// e.g. "alt+1", "ctrl+1"
		for _, prefix := range []string{"alt+", "ctrl+"} {
			if len(s) == len(prefix)+1 {
				ch := s[len(prefix)]
				if s[:len(prefix)] == prefix && ch >= '1' && ch <= '9' {
					return int(ch - '1'), true
				}
			}
		}
	}
	return 0, false
}

type sftpOpenedMsg struct {
	client    *sftp.Client
	hostAlias string
}

func (a App) View() tea.View {
	var view tea.View

	switch a.viewState {
	case LoginView:
		if a.loginModel != nil {
			view = a.loginModel.View()
		} else {
			view = tea.NewView("")
		}
	case MainView:
		// Render tabs from a.tabs (source of truth). The cached a.tabBar can lag and
		// would otherwise show a blank tab row after unlock / single-tab List view.
		layoutW := a.width
		if layoutW <= 0 {
			layoutW = 80
		}
		var tabChrome string
		if len(a.tabs) > 0 {
			tabChrome = a.buildMainTabChrome(layoutW)
		}

		var contentView string
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			tab := a.tabs[a.activeTab]
			if tab.Model != nil {
				contentView = tab.Model.View().Content
			}
		}

		// Ensure contentView fills exactly the allocated height so status bar
		// lands on the last terminal line. TrimRight trailing \n first, then
		// pad or truncate to the exact allocated height.
		allocH := 0
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			allocH = a.mainContentHeightForType(a.tabs[a.activeTab].Type)
		}
		contentView = strings.TrimRight(contentView, "\n")
		cvLines := strings.Count(contentView, "\n") + 1
		if allocH > 0 && cvLines < allocH {
			contentView += strings.Repeat("\n", allocH-cvLines)
		} else if allocH > 0 && cvLines > allocH {
			lines := strings.SplitN(contentView, "\n", cvLines+1)
			if len(lines) > allocH {
				lines = lines[:allocH]
			}
			contentView = strings.Join(lines, "\n")
		}

		statusBar := a.statusBar
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			tab := a.tabs[a.activeTab]
			disc := false
			if tab.Type == SSHTab {
				if sm, ok := tab.Model.(*sshview.Model); ok {
					disc = sm.Disconnected()
				}
			}
			statusBar = statusBar.SetText(mainViewStatusBarHint(a.keyMap, tab.Type, disc))
		}
		statusView := statusBar.View()

		var parts []string
		parts = append(parts, strings.TrimRight(tabChrome, "\n"))
		parts = append(parts, contentView) // already padded to exact allocH; do not trim
		parts = append(parts, strings.TrimRight(statusView, "\n"))
		main := strings.Join(parts, "\n")

		// Overlay: confirm dialog, quick connect, or snippet picker
		if a.confirm.IsActive() {
			overlay := a.confirm.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.quickConnect != nil {
			overlay := a.quickConnect.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.snippetPicker != nil {
			overlay := a.snippetPicker.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.helpOverlay {
			// Keep tab chrome + status bar fixed like SSH/SFTP; only the content band shows the dialog.
			layoutH := a.height
			if layoutH <= 0 {
				layoutH = 24
			}
			tabStr := strings.TrimRight(strings.ReplaceAll(tabChrome, "\r\n", "\n"), "\n")
			statStr := strings.TrimRight(strings.ReplaceAll(statusView, "\r\n", "\n"), "\n")
			topH := lipgloss.Height(tabStr)
			if topH < 1 {
				topH = 1
			}
			stH := lipgloss.Height(statStr)
			if stH < 1 {
				stH = 1
			}
			midH := layoutH - topH - stH
			if midH < 3 {
				midH = 3
			}
			mid := a.helpOverlayPanel(layoutW, midH)
			main = lipgloss.JoinVertical(lipgloss.Left, tabStr, mid, statStr)
		}

		view = tea.NewView(main)
	default:
		view = tea.NewView("")
	}

	// Alternate screen keeps the frame at terminal size; inline mode would grow with
	// content and bubbletea drops overflow from the top, hiding the tab bar.
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (a App) handleHelpOverlayKey(msg tea.KeyPressMsg) (App, tea.Cmd) {
	if key.Matches(msg, a.keyMap.Help) {
		a.helpOverlay = false
		return a, nil
	}
	switch msg.String() {
	case "esc", "escape", "q":
		a.helpOverlay = false
	}
	return a, nil
}

// helpOverlayPanel fills only the middle band (between tab chrome and status bar) with a centered dialog.
func (a App) helpOverlayPanel(layoutW, midH int) string {
	hmap := a.contextualHelpKeyMap()
	innerW := layoutW - 10
	if innerW < 28 {
		innerW = 28
	}
	maxInner := layoutW - 4
	if maxInner < 28 {
		maxInner = 28
	}
	if innerW > maxInner {
		innerW = maxInner
	}
	hb := a.helpBubble
	hb.SetWidth(innerW)
	body := hb.FullHelpView(hmap.FullHelp())
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#eeeeee")).Render("Shortcuts")
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("esc · ? · q  close")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)
	dialog := lipgloss.NewStyle().
		MaxWidth(layoutW - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
	dimBg := lipgloss.NewStyle().Background(lipgloss.Color("#111111"))
	return lipgloss.Place(layoutW, midH, lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceStyle(dimBg))
}

func (a App) openKeysTab() (App, tea.Cmd) {
	km := keyview.New(a.db, a.masterKey)
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
	fm := fwdview.New(a.db)
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
	sm := snippetview.New(a.db)
	if a.width > 0 {
		sm.SetSize(a.width, a.mainContentHeightForType(SnippetTab))
	}
	tab := Tab{Type: SnippetTab, Title: "Snippets", Model: sm}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sm.Init()
}

func (a App) closeCurrentTabIfAllowed() (App, tea.Cmd) {
	if len(a.tabs) > 1 && a.activeTab > 0 {
		if m, ok := a.tabs[a.activeTab].Model.(*sshview.Model); ok {
			_ = m.Close()
		}
		a.tabs = append(a.tabs[:a.activeTab], a.tabs[a.activeTab+1:]...)
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
		a.syncTabBar()
	}
	return a, nil
}

func (a App) lockSession() (App, tea.Cmd) {
	if a.noPasswordMode {
		return a, func() tea.Msg {
			return types.SuccessMsg{Message: "No-password mode stays unlocked"}
		}
	}
	a.masterKey.Lock()
	a.viewState = LoginView
	a.statusBar = a.statusBar.SetLocked(true)
	return a, func() tea.Msg { return types.MasterKeyLockedMsg{} }
}

func (a *App) syncTabBar() {
	items := make([]components.TabItem, len(a.tabs))
	for i, tab := range a.tabs {
		title := tab.Title
		switch tab.Type {
		case SSHTab:
			title = fmt.Sprintf("[S] %s", tab.Title)
		case SFTPTab:
			title = fmt.Sprintf("[F] %s", tab.Title)
		case ForwardTab:
			title = fmt.Sprintf("[P] %s", tab.Title)
		case SnippetTab:
			title = fmt.Sprintf("[B] %s", tab.Title)
		}
		if i < 9 {
			title = fmt.Sprintf("%d:%s", i+1, title)
		}
		items[i] = components.TabItem{Title: title, ID: string(tab.Type)}
	}
	a.tabBar = components.NewTabs(items)
	if len(a.tabs) > 0 {
		if a.activeTab < 0 {
			a.activeTab = 0
		}
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
	}
	a.tabBar = a.tabBar.SetActive(a.activeTab)
	if a.width > 0 {
		a.tabBar = a.tabBar.SetWidth(a.width)
	}
}

func hostDisplayName(host db.Host) string {
	if host.Alias != "" {
		return host.Alias
	}
	if host.Username != "" {
		return fmt.Sprintf("%s@%s", host.Username, host.Hostname)
	}
	return host.Hostname
}

func autoLockTick() tea.Cmd {
	return tea.Tick(1*time.Minute, func(time.Time) tea.Msg {
		return types.AutoLockTickMsg{}
	})
}

func (a App) quitWithCheck() (tea.Model, tea.Cmd) {
	// Count active SSH sessions (SFTP doesn't need confirmation)
	var sshCount int
	for _, tab := range a.tabs {
		if tab.Type == SSHTab {
			if m, ok := tab.Model.(*sshview.Model); ok && !m.Disconnected() {
				sshCount++
			}
		}
	}
	if sshCount == 0 {
		a = a.closeAllForwardSessions()
		return a, tea.Quit
	}
	msg := fmt.Sprintf("%d SSH session(s) still active. Quit anyway?", sshCount)
	a.pendingQuit = true
	a.confirm = components.NewConfirm("Quit eTerm", msg).Show()
	return a, nil
}

func (a *App) processConfirmResult() tea.Cmd {
	confirmed := a.confirm.Result()

	// Handle pending quit
	if a.pendingQuit {
		a.pendingQuit = false
		if confirmed {
			return tea.Quit
		}
		return nil
	}

	// Handle pending delete
	if a.pendingDeleteID > 0 {
		id := a.pendingDeleteID
		a.pendingDeleteID = 0
		if confirmed {
			return func() tea.Msg { return types.HostDeletedMsg{ID: id} }
		}
		return nil
	}

	// Handle pending snippet delete
	if a.pendingSnippetDeleteID > 0 {
		id := a.pendingSnippetDeleteID
		a.pendingSnippetDeleteID = 0
		if confirmed {
			database := a.db
			return func() tea.Msg {
				_ = database.Delete(&db.Snippet{}, id).Error
				return types.SnippetDeletedMsg{ID: id}
			}
		}
		return nil
	}

	// Handle pending forward rule delete
	if a.pendingFwdDeleteID > 0 {
		id := a.pendingFwdDeleteID
		a.pendingFwdDeleteID = 0
		if confirmed {
			database := a.db
			return func() tea.Msg {
				_ = database.Delete(&db.PortForward{}, id).Error
				return types.ForwardRuleDeletedMsg{ID: id}
			}
		}
		return nil
	}

	// Handle pending fingerprint
	if a.pendingFingerprint != nil {
		fp := a.pendingFingerprint
		a.pendingFingerprint = nil
		if confirmed {
			database := a.db
			record := db.HostFingerprint{
				Hostname:    fp.Hostname,
				Port:        fp.Port,
				Algorithm:   fp.Algorithm,
				Fingerprint: fp.Fingerprint,
				TrustedAt:   time.Now(),
			}
			return func() tea.Msg {
				_ = database.Create(&record).Error
				return types.FingerprintAcceptedMsg{
					HostID:        fp.HostID,
					ConnType:      fp.ConnType,
					StreamID:      fp.StreamID,
					ForwardRuleID: fp.ForwardRuleID,
				}
			}
		}
		return func() tea.Msg {
			return types.SuccessMsg{Message: "Connection cancelled"}
		}
	}

	return nil
}
