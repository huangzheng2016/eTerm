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

// ForwardRuleStartMsg asks the app to connect (if needed) and start a saved port-forward rule.
type ForwardRuleStartMsg struct {
	RuleID uint
}

// ForwardRuleStopMsg asks the app to stop a running port-forward rule.
type ForwardRuleStopMsg struct {
	RuleID uint
}

// ForwardRuleResultMsg reports start/stop outcome for UI (running=false means stopped).
type ForwardRuleResultMsg struct {
	RuleID  uint
	Err     error
	Running bool
}

// ForwardRuleDeleteRequestMsg asks the app to confirm before deleting a forward rule.
type ForwardRuleDeleteRequestMsg struct {
	ID   uint
	Desc string
}

// ForwardRuleDeletedMsg is sent after a forward rule is deleted.
type ForwardRuleDeletedMsg struct {
	ID uint
}

// ForwardRuleSavedMsg is sent after a forward rule is created or updated.
type ForwardRuleSavedMsg struct {
	Rule interface{}
}

// SSHReconnectMsg is sent from an SSH tab after a network-style disconnect; App
// redials and replaces the same tab (see openSSHUITabMsg.replaceTabAt).
type SSHReconnectMsg struct {
	HostID   uint
	StreamID uint64
}

type SSHDisconnectMsg struct {
	Err      error
	Alias    string // host display name (toast / fallback matching)
	StreamID uint64 // which sshview.Model ended; 0 means match by Alias+Title only
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

// MasterPasswordChangeMsg requests rotating the master password and re-encrypting stored secrets.
// Current is ignored when the app is in no-password mode.
type MasterPasswordChangeMsg struct {
	Current string
	New     string
}

type ErrorMsg struct {
	Err error
}

// ConnErrorMsg opens the persistent connection-error card. Retry carries the
// tea.Msg to resend when the user presses r (nil means no retry available).
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

// HostDeleteRequestMsg asks the app to show a confirmation dialog before deleting.
type HostDeleteRequestMsg struct {
	ID    uint
	Alias string
}

// HostCloneMsg asks the app to duplicate a host with a unique alias suffix.
type HostCloneMsg struct {
	HostID uint
}

// HostToggleHiddenMsg toggles the "hidden" tag on a host.
type HostToggleHiddenMsg struct {
	HostID uint
}

// AutoLockTickMsg is sent periodically to check master key timeout.
type AutoLockTickMsg struct{}

// FingerprintConfirmMsg asks the user to confirm an unknown host fingerprint.
type FingerprintConfirmMsg struct {
	HostID      uint
	Hostname    string
	Port        int
	Algorithm   string
	Fingerprint string
	// PreviousFingerprint is set when the server key changed vs the DB (user may update trust).
	PreviousFingerprint string
	PreviousAlgorithm   string
	ConnType            string // "ssh", "sftp", "reconnect", "forward"
	StreamID            uint64 // for reconnect
	// ForwardRuleID is set when ConnType is "forward" (port-forward tab dial).
	ForwardRuleID uint
}

// FingerprintAcceptedMsg is sent after the user accepts a fingerprint.
type FingerprintAcceptedMsg struct {
	HostID        uint
	ConnType      string
	StreamID      uint64
	ForwardRuleID uint
}

// QuickConnectRequestMsg triggers the quick connect overlay.
type QuickConnectRequestMsg struct{}

// QuickConnectMsg carries parsed quick connect input.
type QuickConnectMsg struct {
	Hostname string
	Port     int
	Username string
}

// ImportSSHConfigPreviewMsg starts the import flow (conflict prompt when needed).
type ImportSSHConfigPreviewMsg struct{}

// ImportSSHConflictCountMsg reports how many ~/.ssh/config hosts already exist in DB.
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

// ImportSSHConfigRunMsg runs import with strategy: skip | overwrite | merge_jumps.
type ImportSSHConfigRunMsg struct {
	Strategy string
}

// ImportSSHConfigResultMsg reports import results.
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

// OpenSessionHistoryMsg opens saved SSH session transcripts for a host.
type OpenSessionHistoryMsg struct {
	HostID uint
}

type OpenSessionReplayMsg struct {
	HistoryID uint
	Title     string
}

// BatchTagRequestMsg opens batch tag entry for the given hosts (multi-select).
type BatchTagRequestMsg struct {
	HostIDs []uint
}

// BatchActionsRequestMsg opens the batch actions overlay for the given hosts.
type BatchActionsRequestMsg struct {
	HostIDs []uint
}

// BatchActionSelectedMsg chooses the batch action to run for the given hosts.
type BatchActionSelectedMsg struct {
	HostIDs []uint
	Action  string
}

// BatchCommandSubmitMsg starts a read-only batch command.
type BatchCommandSubmitMsg struct {
	HostIDs  []uint
	Command  string
	ReadOnly bool
}

// ExportConfigMsg triggers config export.
type ExportConfigMsg struct{}

// ExportConfigResultMsg reports export results.
type ExportConfigResultMsg struct {
	Path string
	Err  error
}

// SnippetPickerRequestMsg triggers the snippet picker overlay in SSH view.
type SnippetPickerRequestMsg struct{}

// SnippetSelectedMsg carries a selected snippet command to paste into SSH.
type SnippetSelectedMsg struct {
	Command string
}

// ImageUploadProgressMsg reports clipboard upload progress.
type ImageUploadProgressMsg struct {
	StreamID   uint64
	TotalBytes int64
	SentBytes  int64
}

// ImageUploadDoneMsg completes clipboard upload.
type ImageUploadDoneMsg struct {
	StreamID  uint64
	URL       string
	Filename  string
	CacheKey  string
	ExpiresAt time.Time
	Err       error
}

type PasteImageURLMsg struct{}

// SnippetDeleteRequestMsg asks the app to show a confirmation dialog before deleting.
type SnippetDeleteRequestMsg struct {
	ID   uint
	Name string
}

// SnippetDeletedMsg is sent after a snippet is actually deleted.
type SnippetDeletedMsg struct {
	ID uint
}

// SnippetSavedMsg is sent after a snippet is created or updated.
type SnippetSavedMsg struct {
	Snippet interface{}
}

// QuitRequestMsg asks the app to quit with open-session checks.
type QuitRequestMsg struct{}

// CLIConnectMsg triggers a direct connection from CLI arguments.
type CLIConnectMsg struct {
	Hostname string
	Port     int
	Username string
}

// UpdateAvailableMsg notifies that a newer version is available on GitHub.
type UpdateAvailableMsg struct {
	Version string
	URL     string
}

// UpdateCheckDoneMsg reports a forced update check result.
type UpdateCheckDoneMsg struct {
	Version string
	URL     string
	Err     error
}

// UpgradeDownloadDoneMsg completes an async download/extract from GitHub Releases.
type UpgradeDownloadDoneMsg struct {
	Err           error
	Tag           string
	BinaryPath    string
	InstallQuit   bool
	ChecksumsUsed bool // false when SHA256SUMS was absent for this release
}

// EscMenuRequestMsg triggers the ESC menu overlay (QUIT / SETTINGS) on the home view.
type EscMenuRequestMsg struct{}

// OpenImportSourceMenuMsg triggers the import source picker overlay.
type OpenImportSourceMenuMsg struct{}

// OpenSettingsMsg requests opening the settings tab.
type OpenSettingsMsg struct{}

// KeyBindingsChangedMsg notifies that keybindings have been updated and should be reloaded.
type KeyBindingsChangedMsg struct{}

// SettingsSavedMsg reports the result of saving settings.
type SettingsSavedMsg struct {
	Err error
}

// OpenSyncMsg requests opening the sync settings tab.
type OpenSyncMsg struct{}

// SyncStartMsg triggers a manual sync operation.
type SyncStartMsg struct{}

// SyncTickMsg fires periodically to trigger auto-sync.
type SyncTickMsg struct{}

// SyncResultMsg reports the outcome of a sync operation.
type SyncResultMsg struct {
	Pulled int
	Pushed int
	Failed int
	Err    error
}

// SyncTestResultMsg reports the outcome of a sync connection test.
type SyncTestResultMsg struct {
	OK  bool
	Err error
}
