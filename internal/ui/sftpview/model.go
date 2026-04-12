package sftpview

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/sftp"
	"github.com/eterm/eterm/internal/viewkeys"
)

type panelSide int

const (
	leftPanel  panelSide = 0
	rightPanel panelSide = 1
)

// fileItem is rendered by fileDelegate (fixed columns: name | size | date).
type fileItem struct {
	info sftp.FileInfo
}

func (f fileItem) FilterValue() string {
	return f.info.Name
}

type Model struct {
	localList    list.Model
	remoteList   list.Model
	sftpClient   *sftp.Client
	localPath    string
	remotePath   string
	focusedPanel panelSide
	width        int
	height       int
	listInnerH   int // list viewport height passed to bubbles list (for mouse hit-testing)
	transferring bool
	progress     sftp.TransferProgress
	progressCh   chan sftp.TransferProgress
	err          string
	hostAlias    string

	// Confirmation state for delete/rename
	confirmMsg    string       // non-empty = waiting for y/n
	pendingAction func() tea.Cmd // action to run on 'y'

	// Configurable keybindings
	vk viewkeys.SFTPKeys
}

func (m *Model) SetViewKeys(vk viewkeys.SFTPKeys) { m.vk = vk }

func New(client *sftp.Client, hostAlias string, vk viewkeys.SFTPKeys) Model {
	localDelegate := newFileDelegate()
	localList := list.New([]list.Item{}, localDelegate, 0, 0)
	localList.Title = "Local"
	localList.SetFilteringEnabled(true)
	localList.SetShowPagination(false)
	localList.KeyMap.Quit.SetEnabled(false)
	localList.KeyMap.ForceQuit.SetEnabled(false)

	remoteDelegate := newFileDelegate()
	remoteList := list.New([]list.Item{}, remoteDelegate, 0, 0)
	remoteList.Title = "Remote"
	remoteList.SetFilteringEnabled(true)
	remoteList.SetShowPagination(false)
	remoteList.KeyMap.Quit.SetEnabled(false)
	remoteList.KeyMap.ForceQuit.SetEnabled(false)

	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}

	return Model{
		localList:    localList,
		remoteList:   remoteList,
		sftpClient:   client,
		localPath:    home,
		remotePath:   "/",
		focusedPanel: leftPanel,
		hostAlias:    hostAlias,
		progressCh:   make(chan sftp.TransferProgress, 64),
		vk:           vk,
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	panelWidth := w/2 - 2
	if panelWidth < 0 {
		panelWidth = 0
	}
	// Footer is always one line (file counts or progress bar); help is always one line.
	helpH := lipgloss.Height(m.composeHelpLine())
	if helpH < 1 {
		helpH = 1
	}
	footerH := 1 // composeFooter always returns at least one line now
	pageH := 1   // pagination indicator line
	panelOuter := h - helpH - footerH - pageH
	if panelOuter < 1 {
		panelOuter = 1
	}
	innerW := panelListInnerWidth(panelWidth)
	innerH := panelListInnerHeight(panelOuter)
	m.listInnerH = innerH
	m.localList.SetSize(innerW, innerH)
	m.remoteList.SetSize(innerW, innerH)
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
