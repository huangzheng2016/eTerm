package app

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type tmuxTerminalOpenedMsg struct {
	is      *internalssh.InteractiveSession
	title   string
	session string
}

func (a App) loadTmuxSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := tmux.ListSessions(context.Background())
		return types.TmuxSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func (a App) openTmux(msg types.TmuxOpenMsg) (App, tea.Cmd) {
	cols, rows := ptyFromAppSizeForTab(a, LocalTab)
	return a, func() tea.Msg {
		if msg.New {
			is, name, err := tmux.NewSession(context.Background(), rows, cols)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("tmux new-session: %w", err)}
			}
			return tmuxTerminalOpenedMsg{is: is, title: tmuxTabTitle(name), session: name}
		}
		is, err := appAttachTmuxSession(context.Background(), msg.Name, rows, cols)
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("tmux attach-session: %w", err)}
		}
		return tmuxTerminalOpenedMsg{is: is, title: tmuxTabTitle(msg.Name), session: msg.Name}
	}
}

func (a App) applyTmuxTerminalOpened(msg tmuxTerminalOpenedMsg) (App, tea.Cmd) {
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(LocalTab))
	}
	tab := Tab{Type: LocalTab, Title: msg.title, Model: sv, TmuxSession: msg.session}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
	return a, sv.Init()
}

func (a App) killTmuxSession(msg types.TmuxKillMsg) tea.Cmd {
	name := msg.Name
	return func() tea.Msg {
		if err := tmux.KillSession(context.Background(), name); err != nil {
			return types.TmuxSessionsLoadedMsg{Err: err}
		}
		sessions, err := tmux.ListSessions(context.Background())
		return types.TmuxSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func (a App) renameTmuxSession(msg types.TmuxRenameMsg) (App, tea.Cmd) {
	oldName := strings.TrimSpace(msg.OldName)
	newName := strings.TrimSpace(msg.NewName)
	if oldName == "" || newName == "" {
		return a, nil
	}
	return a, func() tea.Msg {
		if err := tmux.RenameSession(context.Background(), oldName, newName); err != nil {
			return types.TmuxSessionsLoadedMsg{Err: err}
		}
		return tmuxRenameAppliedMsg{OldName: oldName, NewName: newName}
	}
}

func (a *App) renameTmuxTabs(oldName, newName string) {
	for i := range a.tabs {
		if a.tabs[i].TmuxSession == oldName {
			a.tabs[i].Title = tmuxTabTitle(newName)
			a.tabs[i].TmuxSession = newName
		}
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
}

func tmuxTabTitle(name string) string {
	return "[T]" + name
}
