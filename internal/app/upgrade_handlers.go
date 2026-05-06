package app

import (
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/version"

	tea "charm.land/bubbletea/v2"
)

func (a App) dismissUpgradePrompt(saveDismissedTag bool) (App, tea.Cmd) {
	if a.upgradePrompt == nil {
		return a, nil
	}
	if saveDismissedTag && a.upgradePrompt.Tag != "" {
		_ = db.SetSetting(a.db, version.SettingUpgradeDismissedTag, a.upgradePrompt.Tag)
	}
	a.upgradePrompt = nil
	return a, nil
}

func (a App) handleUpgradePromptKey(msg tea.KeyPressMsg) (App, tea.Cmd) {
	p := a.upgradePrompt
	if p == nil || p.Busy {
		return a, nil
	}
	switch msg.String() {
	case "up", "k":
		p.Cursor--
		p.clampCursor()
		return a, nil
	case "down", "j":
		p.Cursor++
		p.clampCursor()
		return a, nil
	case "esc", "escape":
		return a.dismissUpgradePrompt(true)
	case "enter":
		return a.activateUpgradeChoice()
	case "i":
		if !p.SupportedArch {
			return a, nil
		}
		p.Cursor = 0
		return a.activateUpgradeChoice()
	case "d":
		if !p.SupportedArch {
			return a, nil
		}
		p.Cursor = 1
		return a.activateUpgradeChoice()
	case "o":
		if p.SupportedArch {
			p.Cursor = 2
		} else {
			p.Cursor = 0
		}
		return a.activateUpgradeChoice()
	default:
		return a, nil
	}
}

func (a App) activateUpgradeChoice() (App, tea.Cmd) {
	p := a.upgradePrompt
	if p == nil || p.Busy {
		return a, nil
	}
	if !p.SupportedArch {
		switch p.Cursor {
		case 0:
			if err := browseReleaseURL(p.ReleaseURL); err != nil {
				var tc tea.Cmd
				a.toast, tc = a.toast.Show(err.Error(), components.ToastWarning, 4*time.Second)
				return a, tc
			}
			return a, nil
		case 1:
			return a.dismissUpgradePrompt(true)
		}
		return a, nil
	}

	switch p.Cursor {
	case 0:
		p.Busy = true
		p.BusyHint = "Downloading and verifying..."
		return a, upgradeArchiveCmd(p.Tag, p.Archive, p.Inner, true)
	case 1:
		p.Busy = true
		p.BusyHint = "Downloading and verifying..."
		return a, upgradeArchiveCmd(p.Tag, p.Archive, p.Inner, false)
	case 2:
		if err := browseReleaseURL(p.ReleaseURL); err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(err.Error(), components.ToastWarning, 4*time.Second)
			return a, tc
		}
		return a, nil
	case 3:
		return a.dismissUpgradePrompt(true)
	}
	return a, nil
}

func (a App) upgradePromptMouse(lx, ly int) (tea.Model, tea.Cmd) {
	p := a.upgradePrompt
	if p == nil || p.Busy {
		return a, nil
	}
	idx := ly - upgradePromptItemRow
	if idx < 0 || idx >= p.menuLen() {
		return a, nil
	}
	p.Cursor = idx
	next, cmd := a.activateUpgradeChoice()
	return next, cmd
}

func (a App) handleUpgradeDownloadDone(msg types.UpgradeDownloadDoneMsg) (App, tea.Cmd) {
	if a.upgradePrompt != nil {
		a.upgradePrompt.Busy = false
		a.upgradePrompt.BusyHint = ""
	}
	if msg.Err != nil {
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.Err.Error(), components.ToastError, 8*time.Second)
		return a, tc
	}
	sumNote := ""
	if !msg.ChecksumsUsed {
		sumNote = " (SHA256SUMS missing; skipped verify)"
	}
	if msg.InstallQuit {
		target, err := version.RunningExecutablePath()
		if err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(err.Error(), components.ToastError, 6*time.Second)
			return a, tc
		}
		if err := version.ScheduleDeferredReplace(msg.BinaryPath, target); err != nil {
			var tc tea.Cmd
			a.toast, tc = a.toast.Show(err.Error(), components.ToastError, 6*time.Second)
			return a, tc
		}
		a.upgradePrompt = nil
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("Update scheduled. Exit now."+sumNote, components.ToastSuccess, 3*time.Second)
		return a, tea.Sequence(tc, tea.Tick(900*time.Millisecond, func(time.Time) tea.Msg {
			return tea.Quit()
		}))
	}
	dirHint := ""
	if err := browseDirectoryForFile(msg.BinaryPath); err == nil {
		dirHint = " Opened folder."
	}
	a.upgradePrompt = nil
	var tc tea.Cmd
	a.toast, tc = a.toast.Show("Saved: "+msg.BinaryPath+sumNote+dirHint, components.ToastSuccess, 6*time.Second)
	return a, tc
}
