package app

import (
	"github.com/huangzheng2016/eTerm/internal/sftp"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

type openSSHUITabMsg struct {
	is           *internalssh.InteractiveSession
	alias        string
	hostID       uint
	historyID    uint
	initialCommands []string
	replaceTabAt int // append when < 0; otherwise replace a.tabs[replaceTabAt]
}

type sftpOpenedMsg struct {
	client    *sftp.Client
	hostAlias string
}
