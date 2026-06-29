package app

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/localtmux"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type tmuxTerminalOpenedMsg struct {
	is    *internalssh.InteractiveSession
	title string
}

func (a App) loadTmuxSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := localtmux.ListSessions(context.Background())
		return types.TmuxSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func (a App) openTmux(msg types.TmuxOpenMsg) (App, tea.Cmd) {
	cols, rows := ptyFromAppSizeForTab(a, LocalTab)
	return a, func() tea.Msg {
		if msg.New {
			is, name, err := localtmux.NewSession(context.Background(), rows, cols)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("tmux new-session: %w", err)}
			}
			return tmuxTerminalOpenedMsg{is: is, title: tmuxTabTitle(name)}
		}
		is, err := localtmux.AttachSession(context.Background(), msg.Name, rows, cols)
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("tmux attach-session: %w", err)}
		}
		return tmuxTerminalOpenedMsg{is: is, title: tmuxTabTitle(msg.Name)}
	}
}

func (a App) applyTmuxTerminalOpened(msg tmuxTerminalOpenedMsg) (App, tea.Cmd) {
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(LocalTab))
	}
	tab := Tab{Type: LocalTab, Title: msg.title, Model: sv}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, sv.Init()
}

func (a App) killTmuxSession(msg types.TmuxKillMsg) tea.Cmd {
	name := msg.Name
	return func() tea.Msg {
		if err := localtmux.KillSession(context.Background(), name); err != nil {
			return types.ErrorMsg{Err: err}
		}
		sessions, err := localtmux.ListSessions(context.Background())
		return types.TmuxSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func (a App) renameTmuxSession(msg types.TmuxRenameMsg) (App, tea.Cmd) {
	oldName := strings.TrimSpace(msg.OldName)
	newName := strings.TrimSpace(msg.NewName)
	if oldName == "" || newName == "" {
		return a, nil
	}
	for i := range a.tabs {
		if a.tabs[i].Title == tmuxTabTitle(oldName) {
			a.tabs[i].Title = tmuxTabTitle(newName)
		}
	}
	a.syncTabBar()
	return a, func() tea.Msg {
		if err := localtmux.RenameSession(context.Background(), oldName, newName); err != nil {
			return types.ErrorMsg{Err: err}
		}
		sessions, err := localtmux.ListSessions(context.Background())
		return types.TmuxSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func tmuxTabTitle(name string) string {
	return "[T]" + name
}
