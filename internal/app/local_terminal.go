package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type localTerminalOpenedMsg struct {
	is    *internalssh.InteractiveSession
	title string
}

func (a App) openLocalTerminal() (App, tea.Cmd) {
	database := a.db
	cols, rows := ptyFromAppSizeForTab(a, LocalTab)
	return a, func() tea.Msg {
		configured, _ := db.GetSetting(database, localterm.SettingShell)
		shell := localterm.DefaultShell(configured)
		is, err := localterm.NewSession(shell, rows, cols)
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("local terminal: %w", err)}
		}
		title := filepath.Base(shell)
		if title == "." || title == "/" || title == "" {
			title = "local"
		}
		return localTerminalOpenedMsg{is: is, title: title}
	}
}

func (a App) applyLocalTerminalOpened(msg localTerminalOpenedMsg) (App, tea.Cmd) {
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	sv.SetHistoryID(createLocalSessionHistory(a.db, msg.title, "local"))
	configureSessionCapture(a.db, sv)
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(LocalTab))
	}
	tab := Tab{Type: LocalTab, Title: msg.title, Model: sv}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sv.Init()
}
