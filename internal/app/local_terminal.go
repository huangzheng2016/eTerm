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
	"gorm.io/gorm"
)

type localTerminalOpenedMsg struct {
	is    *internalssh.InteractiveSession
	title string
}

func localShell(database *gorm.DB) string {
	configured, _ := db.GetSetting(database, localterm.SettingShell)
	return localterm.DefaultShell(configured)
}

func localShellTitle(database *gorm.DB) string {
	title := filepath.Base(localShell(database))
	if title == "." || title == "/" || title == "" {
		title = "local"
	}
	return title
}

func (a App) openLocalTerminal() (App, tea.Cmd) {
	database := a.db
	cols, rows := ptyFromAppSizeForTab(a, LocalTab)
	return a, func() tea.Msg {
		is, err := localterm.NewSession(localShell(database), rows, cols)
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("local terminal: %w", err)}
		}
		return localTerminalOpenedMsg{is: is, title: localShellTitle(database)}
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
