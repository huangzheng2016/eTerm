package app

import (
	"time"

	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/syncblob"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/remotemenu"
	"github.com/huangzheng2016/eTerm/internal/ui/shareview"
	"github.com/huangzheng2016/eTerm/internal/ui/tmuxmenu"
	"github.com/huangzheng2016/eTerm/internal/voice"

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
	LocalTab          TabType = "local"
	BatchResultTab    TabType = "batch-result"
	SettingsTab       TabType = "settings"
	SyncTab           TabType = "sync"
	SessionHistoryTab TabType = "session-hist"
	SessionListTab    TabType = "sessions"
	SessionReplayTab  TabType = "session-replay"
)

type Tab struct {
	Type          TabType
	Title         string
	Model         tea.Model
	TmuxSession   string
	tmuxRestoreID uint64
	userRenamed   bool
}

type App struct {
	db             *gorm.DB
	masterKey      *security.MasterKeyManager
	noPasswordMode bool
	viewState      ViewState
	tabs           []Tab
	activeTab      int
	winFocused     bool
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

	pendingDeleteID        uint
	pendingSnippetDeleteID uint
	pendingFwdDeleteID     uint
	pendingFingerprint     *types.FingerprintConfirmMsg
	pendingQuit            bool
	pendingTmuxRestore     []tmuxRestoreEntry
	tmuxRestoreDeferred    bool
	pendingRemoteTmuxKill  *types.RemoteTmuxKillMsg
	pendingTmuxKill        *types.TmuxKillMsg

	quickConnect *quickConnectModel

	snippetPicker *snippetPickerModel

	helpOverlay bool

	pendingCLIConnect *CLIConnectInfo

	forwardByHost map[uint]*hostForwardState

	escMenu *escMenuModel

	kbConfig KeyBindingConfig

	noUpdateCheck    bool
	forceUpdateCheck bool

	syncing bool

	batchTag     *batchTagModel
	batchActions *batchActionsModel

	importSourceMenu *importSourceMenuModel
	importHostList   *importHostListModel
	importKeyList    *importKeyListModel

	upgradePrompt *upgradePromptModel

	commandPalette *commandPaletteModel

	aiView    *aiview.Model
	aiBridge  *aiBridge
	aiVisible bool
	aiToolCh  chan aiToolRequest
	aiShared  *aiSharedState

	voiceEngine        voice.Engine
	voiceCfg           voiceSettings
	voiceCfgLoaded     bool
	voiceName          string
	voiceRec           bool
	voiceBusy          bool
	voiceStartedAt     time.Time
	voicePartial       string
	voiceDropNotified  bool
	voiceTickSeq       int
	voiceProgressCh    chan float64
	voiceProgressArmed bool
	voiceMake          func(voiceSettings, func(float64)) (voice.Engine, error)
	voiceSettingsView  *voiceSettingsModel
	voiceTest          bool
	voiceTestSeq       int
	voiceSwallowFinal  bool
	voiceDlCh          chan voiceDownloadMsg
	voiceDlActive      bool
	voiceReady         func(voiceSettings) bool
	voiceDownload      func(string, func(float64)) error

	connError *connErrorModel

	remoteMenu *remotemenu.Model
	tmuxMenu   *tmuxmenu.Model

	renamePrompt *sessionRenameModel
	sharePrompt  *shareview.Model

	connectProgressSeq uint64
	tmuxRestoreSeq     uint64

	pendingBatchSnippetHostIDs []uint
	pendingBatchOpenHosts      []uint
	pendingQuickConnect        *types.QuickConnectMsg
	blobUploadProgressCh       chan syncblob.Progress
	blobURLCache               map[string]blobURLCacheEntry
	tmuxRestorePath            string
}

type blobURLCacheEntry struct {
	URL       string
	Filename  string
	ExpiresAt time.Time
}

func NewApp(database *gorm.DB, masterKey *security.MasterKeyManager) App {
	tabs := []Tab{}
	tabBar := components.NewTabs([]components.TabItem{})

	kbCfg := LoadKeyBindingConfig(database)

	return App{
		db:              database,
		masterKey:       masterKey,
		viewState:       LoginView,
		tabs:            tabs,
		activeTab:       0,
		tabBar:          tabBar,
		statusBar:       components.NewStatusBar(),
		helpBubble:      newAppHelpBubble(),
		toast:           components.NewToast(),
		confirm:         components.NewConfirm("", ""),
		keyMap:          BuildKeyMap(kbCfg),
		kbConfig:        kbCfg,
		tmuxRestorePath: defaultTmuxRestorePath(),
		aiToolCh:        make(chan aiToolRequest, 16),
		aiShared:        &aiSharedState{},
		winFocused:      true,
	}
}

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

func (a App) SetForceUpdateCheck(v bool) App {
	a.forceUpdateCheck = v
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
