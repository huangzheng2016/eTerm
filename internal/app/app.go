package app

import (
	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"

	bubbleshelp "charm.land/bubbles/v2/help"
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
	HomeTab           TabType = "home"
	EditorTab         TabType = "editor"
	SFTPTab           TabType = "sftp"
	KeyTab            TabType = "key"
	ForwardTab        TabType = "fwd"
	FwdEditorTab      TabType = "fwd-editor"
	SnippetTab        TabType = "snippets"
	SnippetEditorTab  TabType = "snippet-editor"
	SSHTab            TabType = "ssh"
	SettingsTab       TabType = "settings"
	SyncTab           TabType = "sync"
	SessionHistoryTab TabType = "session-hist"
)

type Tab struct {
	Type  TabType
	Title string
	Model tea.Model
}

type App struct {
	db             *gorm.DB
	masterKey      *security.MasterKeyManager
	noPasswordMode bool
	viewState      ViewState
	tabs           []Tab
	activeTab      int
	tabBar         components.TabsModel
	statusBar      components.StatusBar
	helpBubble     bubbleshelp.Model
	toast          components.ToastModel
	confirm        components.ConfirmModel
	keyMap         KeyMap
	width          int
	height         int
	loginModel     tea.Model
	initCmd        tea.Cmd

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

	// ESC menu overlay
	escMenu *escMenuModel

	// Keybinding configuration
	kbConfig KeyBindingConfig

	noUpdateCheck bool

	syncing bool // true while runSync() is in flight

	batchTag *batchTagModel

	importStratMenu *importStratMenuModel
}

func NewApp(database *gorm.DB, masterKey *security.MasterKeyManager) App {
	tabs := []Tab{}
	tabBar := components.NewTabs([]components.TabItem{})

	kbCfg := LoadKeyBindingConfig(database)

	return App{
		db:         database,
		masterKey:  masterKey,
		viewState:  LoginView,
		tabs:       tabs,
		activeTab:  0,
		tabBar:     tabBar,
		statusBar:  components.NewStatusBar(),
		helpBubble: newAppHelpBubble(),
		toast:      components.NewToast(),
		confirm:    components.NewConfirm("", ""),
		keyMap:     BuildKeyMap(kbCfg),
		kbConfig:   kbCfg,
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

func (a App) SetNoUpdateCheck(v bool) App {
	a.noUpdateCheck = v
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
