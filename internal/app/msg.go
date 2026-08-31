package app

import (
	"github.com/huangzheng2016/eTerm/internal/sftp"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/voice"
)

type openSSHUITabMsg struct {
	is              *internalssh.InteractiveSession
	alias           string
	hostID          uint
	historyID       uint
	initialCommands []string
	replaceTabAt    int // append when < 0; otherwise replace a.tabs[replaceTabAt]
}

type sftpOpenedMsg struct {
	client    *sftp.Client
	hostAlias string
}

type connectProgressMsg struct {
	Seq  uint64
	Text string
	Next <-chan string
}

type remoteTerminalOpenedMsg struct {
	is           *internalssh.InteractiveSession
	title        string
	tabType      TabType
	replaceTabAt int // append when < 0; otherwise replace a.tabs[replaceTabAt]
	reconnect    *types.RemoteReconnect
	background   bool
	resume       bool // reattach to the existing tab model, keeping scrollback
}

type remoteTmuxRenameAppliedMsg struct {
	Peer         types.RemotePeer
	OldSessionID string
	Name         string
}

type tmuxRenameAppliedMsg struct {
	OldName string
	NewName string
}

type voiceEventMsg struct{ ev voice.Event }
type voiceProgressMsg struct{ pct float64 }
type voiceStartedMsg struct{}
type voiceStoppedMsg struct{}
type voiceStartFailedMsg struct{ err error }
type voiceTickMsg struct{ seq int }
type voiceEngineClosedMsg struct{}
type openVoiceSettingsMsg struct{}

// voiceSettingsChangedMsg carries a saved voice config; keepEngine avoids an
// engine rebuild when only delivery-time or VAD settings changed.
type voiceSettingsChangedMsg struct {
	cfg        voiceSettings
	keepEngine bool
}
