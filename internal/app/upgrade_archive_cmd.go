package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/version"
)

func upgradeArchiveCmd(tag, archiveBase, inner string, installQuit bool) tea.Cmd {
	return func() tea.Msg {
		binPath, sumsUsed, err := version.DownloadUpgradeArchive(tag, archiveBase, inner)
		return types.UpgradeDownloadDoneMsg{
			Err:           err,
			Tag:           tag,
			BinaryPath:    binPath,
			InstallQuit:   installQuit,
			ChecksumsUsed: sumsUsed,
		}
	}
}
