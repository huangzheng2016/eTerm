package types

import (
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

type SwitchTabMsg struct {
	Index int
}

type NewTabMsg struct {
	Type  string
	Title string
	Data  interface{}
}

type CloseTabMsg struct {
	Index int
}

type SSHConnectMsg struct {
	HostID uint
}

type ForwardRuleStartMsg struct {
	RuleID uint
}

type ForwardRuleStopMsg struct {
	RuleID uint
}

type ForwardRuleResultMsg struct {
	RuleID  uint
	Err     error
	Running bool
}

type ForwardRuleDeleteRequestMsg struct {
	ID   uint
	Desc string
}

type ForwardRuleDeletedMsg struct {
	ID uint
}

type ForwardRuleSavedMsg struct {
	Rule interface{}
}

type SSHReconnectMsg struct {
	HostID   uint
	StreamID uint64
}

type SSHDisconnectMsg struct {
	Err      error
	Alias    string
	StreamID uint64
}

type SFTPOpenMsg struct {
	HostID uint
}

type RemotePeer struct {
	ID   string
	Name string
}

type RemoteHost struct {
	SyncID   string `json:"sync_id"`
	Alias    string `json:"alias"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Tags     string `json:"tags"`
	Group    string `json:"group"`
}

type RemoteDaemonLoadingMsg struct {
	Silent bool
}

type RemoteDaemonLoadedMsg struct {
	Peers  []RemotePeer
	Hosts  []RemoteHost
	Err    error
	Silent bool
}

type RemoteDaemonRefreshMsg struct{}

type RefreshConnectivityMsg struct{}

type RemotePeerMenuMsg struct {
	Peer  RemotePeer
	Hosts []RemoteHost
}

type RemoteShareMsg struct {
	Peer      RemotePeer
	Target    string
	SessionID string
	Label     string
}

type RemoteShareSubmitMsg struct {
	Peer      RemotePeer
	Target    string
	SessionID string
	Label     string
	Name      string
	MaxHours  int
}

type RemoteShellOpenMsg struct {
	Peer       RemotePeer
	Target     string
	HostSyncID string
	HostLabel  string
	Tmux       bool
	SessionID  string
}

type RemoteTmuxSessionsLoadedMsg struct {
	Peer     RemotePeer
	Sessions []relay.TmuxSessionInfo
	Err      error
}

type RemoteTmuxKillMsg struct {
	Peer      RemotePeer
	SessionID string
}

type RemoteTmuxKillRequestMsg struct {
	Peer      RemotePeer
	SessionID string
}

type RemoteTmuxRenameRequestMsg struct {
	Peer        RemotePeer
	SessionID   string
	CurrentName string
}

type RemoteTmuxRenameMsg struct {
	Peer      RemotePeer
	SessionID string
	Name      string
}

type RemoteReconnect struct {
	Peer       RemotePeer
	Target     string
	HostSyncID string
	SessionID  string
	Tmux       bool
}

type RemoteShellReconnectMsg struct {
	StreamID    uint64
	Spec        RemoteReconnect
	Auto        bool
	Attempt     int
	MaxAttempts int
}

type MasterKeyUnlockedMsg struct {
	Salt       string
	Verifier   string
	IsSetup    bool
	NoPassword bool
}

type MasterKeyLockedMsg struct{}

type MasterPasswordChangeMsg struct {
	Current string
	New     string
}

type ErrorMsg struct {
	Err error
}

type ConnErrorMsg struct {
	Err    error
	Target string
	Retry  any
}

type SuccessMsg struct {
	Message string
}

type RefreshListMsg struct{}

type TmuxSession = relay.TmuxSessionInfo

type TmuxMenuMsg struct{}

type TmuxSessionsLoadedMsg struct {
	Sessions []TmuxSession
	Err      error
}

type TmuxOpenMsg struct {
	Name string
	New  bool
}

type TmuxKillRequestMsg struct {
	Name string
}

type TmuxKillMsg struct {
	Name string
}

type TmuxRenameRequestMsg struct {
	Name string
}

type TmuxRenameMsg struct {
	OldName string
	NewName string
}

type HostDeletedMsg struct {
	ID uint
}

type HostSavedMsg struct {
	Host interface{}
}

type HostDeleteRequestMsg struct {
	ID    uint
	Alias string
}

type HostCloneMsg struct {
	HostID uint
}

type HostToggleHiddenMsg struct {
	HostID uint
}

type AutoLockTickMsg struct{}

type FingerprintConfirmMsg struct {
	HostID              uint
	Hostname            string
	Port                int
	Algorithm           string
	Fingerprint         string
	PreviousFingerprint string
	PreviousAlgorithm   string
	ConnType            string
	StreamID            uint64
	ForwardRuleID       uint
}

type FingerprintAcceptedMsg struct {
	HostID        uint
	ConnType      string
	StreamID      uint64
	ForwardRuleID uint
}

type QuickConnectRequestMsg struct{}

type QuickConnectMsg struct {
	Hostname string
	Port     int
	Username string
}

type ImportSSHConfigPreviewMsg struct{}

type ImportSSHConflictCountMsg struct {
	Count int
}

type ImportSSHConfigPreviewResultMsg struct {
	Added       int
	Changed     int
	Skipped     int
	KeysAdded   int
	KeysSkipped int
	KeysFailed  int
	Err         error
}

type ImportSSHConfigRunMsg struct {
	Strategy string
}

type ImportSSHConfigResultMsg struct {
	Imported             int
	Skipped              int
	Overwritten          int
	UnresolvedProxyJumps int
	KeysImported         int
	KeysSkipped          int
	KeysFailed           int
	Err                  error
}

type OpenSessionHistoryMsg struct {
	HostID uint
}

type OpenSessionReplayMsg struct {
	HistoryID uint
	Title     string
}

type BatchTagRequestMsg struct {
	HostIDs []uint
}

type BatchActionsRequestMsg struct {
	HostIDs []uint
}

type BatchActionSelectedMsg struct {
	HostIDs []uint
	Action  string
}

type BatchCommandSubmitMsg struct {
	HostIDs  []uint
	Command  string
	ReadOnly bool
}

type ExportConfigMsg struct{}

type ExportConfigResultMsg struct {
	Path string
	Err  error
}

type SnippetPickerRequestMsg struct{}

type SnippetSelectedMsg struct {
	Command string
}

type BlobUploadProgressMsg struct {
	StreamID   uint64
	TotalBytes int64
	SentBytes  int64
}

type BlobUploadDoneMsg struct {
	StreamID  uint64
	URL       string
	Filename  string
	CacheKey  string
	ExpiresAt time.Time
	Err       error
}

type PasteBlobURLMsg struct{}

type SnippetDeleteRequestMsg struct {
	ID   uint
	Name string
}

type SnippetDeletedMsg struct {
	ID uint
}

type SnippetSavedMsg struct {
	Snippet interface{}
}

type QuitRequestMsg struct{}

type CLIConnectMsg struct {
	Hostname string
	Port     int
	Username string
}

type UpdateAvailableMsg struct {
	Version string
	URL     string
}

type UpdateCheckDoneMsg struct {
	Version string
	URL     string
	Err     error
}

type UpgradeDownloadDoneMsg struct {
	Err           error
	Tag           string
	BinaryPath    string
	InstallQuit   bool
	ChecksumsUsed bool
}

type EscMenuRequestMsg struct{}

type OpenImportSourceMenuMsg struct{}

type OpenSettingsMsg struct{}

type KeyBindingsChangedMsg struct{}

type SettingsSavedMsg struct {
	Err error
}

type OpenSyncMsg struct{}

type SyncStartMsg struct{}

type SyncTickMsg struct{}

type SyncResultMsg struct {
	Pulled int
	Pushed int
	Failed int
	Err    error
}

type SyncTestResultMsg struct {
	OK  bool
	Err error
}
