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
	replaceTabAt    int
	tmuxSession     string
	configWarn      bool
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
	replaceTabAt int
	reconnect    *types.RemoteReconnect
	background   bool
	resume       bool
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

type voiceDownloadRequestMsg struct{ target string }

type voiceDownloadMsg struct {
	target string
	pct    float64
	err    error
	done   bool
}

type voiceTestRequestMsg struct{ stop bool }
type voiceTestTimeoutMsg struct{ seq int }

type voiceHelperUpdateCheckRequestMsg struct{}

type voiceHelperUpdateCheckMsg struct {
	tag string
	err error
}

type voiceSettingsChangedMsg struct {
	cfg        voiceSettings
	keepEngine bool
}
