package app

import (
	"github.com/huangzheng2016/eTerm/internal/sftp"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
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
}
