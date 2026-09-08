package sftpview

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/sftp"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type panelSide int

const (
	leftPanel  panelSide = 0
	rightPanel panelSide = 1
)

type fileItem struct {
	info sftp.FileInfo
}

func (f fileItem) FilterValue() string {
	return f.info.Name
}

type Model struct {
	localList       list.Model
	remoteList      list.Model
	sftpClient      *sftp.Client
	localPath       string
	remotePath      string
	localPathInput  textinput.Model
	remotePathInput textinput.Model
	pathInputActive bool
	focusedPanel    panelSide
	width           int
	height          int
	listInnerH      int
	transferring    bool
	progress        sftp.TransferProgress
	progressCh      chan sftp.TransferProgress
	err             string
	hostAlias       string

	confirmMsg        string
	pendingAction     func() tea.Cmd
	chmodInput        textinput.Model
	chmodPath         string
	chmodActive       bool
	nameInput         textinput.Model
	namePromptKind    string
	namePromptOldName string
	namePromptActive  bool

	vk viewkeys.SFTPKeys
}

func (m *Model) SetViewKeys(vk viewkeys.SFTPKeys) { m.vk = vk }

func (m Model) Close() error {
	if m.sftpClient == nil {
		return nil
	}
	return m.sftpClient.Close()
}

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

	localPathInput := textinput.New()
	localPathInput.Prompt = "Local: "
	localPathInput.SetValue(home)
	localPathInput.Blur()
	remotePathInput := textinput.New()
	remotePathInput.Prompt = "Remote: "
	remotePathInput.SetValue("/")
	remotePathInput.Blur()

	chmodInput := textinput.New()
	chmodInput.Placeholder = "0644"
	chmodInput.CharLimit = 4
	nameInput := textinput.New()
	nameInput.Placeholder = "name"
	nameInput.CharLimit = 255

	return Model{
		localList:       localList,
		remoteList:      remoteList,
		sftpClient:      client,
		localPath:       home,
		remotePath:      "/",
		localPathInput:  localPathInput,
		remotePathInput: remotePathInput,
		focusedPanel:    leftPanel,
		hostAlias:       hostAlias,
		progressCh:      make(chan sftp.TransferProgress, 64),
		chmodInput:      chmodInput,
		nameInput:       nameInput,
		vk:              vk,
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	panelWidth := w/2 - 2
	if panelWidth < 0 {
		panelWidth = 0
	}
	helpH := lipgloss.Height(m.composeHelpLine())
	if helpH < 1 {
		helpH = 1
	}
	footerH := 1
	pageH := 1
	panelOuter := h - helpH - footerH - pageH
	if panelOuter < 1 {
		panelOuter = 1
	}
	innerW := panelListInnerWidth(panelWidth)
	innerH := panelListInnerHeight(panelOuter)
	m.listInnerH = innerH
	m.localList.SetSize(innerW, innerH)
	m.remoteList.SetSize(innerW, innerH)
	m.localPathInput.SetWidth(innerW)
	m.remotePathInput.SetWidth(innerW)
	cw := w - 16
	if cw < 12 {
		cw = 12
	}
	if cw > 24 {
		cw = 24
	}
	m.chmodInput.SetWidth(cw)
	m.nameInput.SetWidth(cw)
	m.updatePathTitles()
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
