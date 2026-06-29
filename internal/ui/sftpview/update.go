package sftpview

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"strings"

	"github.com/huangzheng2016/eTerm/internal/sftp"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type localFilesMsg struct {
	files []sftp.FileInfo
	err   error
}

type remoteFilesMsg struct {
	files []sftp.FileInfo
	err   error
}

type transferCompleteMsg struct {
	err error
}

type transferProgressMsg struct {
	progress sftp.TransferProgress
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadLocalFiles(), m.loadRemoteFiles())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case localFilesMsg:
		m.localPathInput.SetValue(m.localPath)
		m.updatePathTitles()
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		items := filesToItems(msg.files)
		cmd := m.localList.SetItems(items)
		return m, cmd

	case remoteFilesMsg:
		m.remotePathInput.SetValue(m.remotePath)
		m.updatePathTitles()
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		items := filesToItems(msg.files)
		cmd := m.remoteList.SetItems(items)
		return m, cmd

	case transferProgressMsg:
		m.progress = msg.progress
		if msg.progress.Done {
			m.transferring = false
			return m, nil
		}
		return m, waitProgress(m.progressCh)

	case transferCompleteMsg:
		m.transferring = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		return m, tea.Batch(m.loadLocalFiles(), m.loadRemoteFiles())

	case tea.KeyPressMsg:
		if m.namePromptActive {
			switch msg.String() {
			case "esc":
				m.closeNamePrompt()
				return m, nil
			case "enter":
				return m, m.applyNamePrompt()
			}
			var cmd tea.Cmd
			m.nameInput, cmd = m.nameInput.Update(msg)
			return m, cmd
		}

		if m.chmodActive {
			switch msg.String() {
			case "esc":
				m.chmodActive = false
				m.chmodPath = ""
				return m, nil
			case "enter":
				return m, m.chmodCmd()
			}
			var cmd tea.Cmd
			m.chmodInput, cmd = m.chmodInput.Update(msg)
			return m, cmd
		}

		// Handle confirmation prompt
		if m.confirmMsg != "" {
			switch msg.String() {
			case "y", "Y":
				action := m.pendingAction
				m.confirmMsg = ""
				m.pendingAction = nil
				if action != nil {
					return m, action()
				}
			case "n", "N", "esc":
				m.confirmMsg = ""
				m.pendingAction = nil
			}
			return m, nil
		}

		if m.pathInputActive {
			switch msg.String() {
			case "esc":
				m.cancelPathInput()
				return m, nil
			case "enter":
				return m, m.applyPathInput()
			}
			var cmd tea.Cmd
			if m.focusedPanel == leftPanel {
				m.localPathInput, cmd = m.localPathInput.Update(msg)
			} else {
				m.remotePathInput, cmd = m.remotePathInput.Update(msg)
			}
			m.updatePathTitles()
			return m, cmd
		}

		if m.focusedPanel == leftPanel && m.localList.FilterState() == list.Filtering {
			break
		}
		if m.focusedPanel == rightPanel && m.remoteList.FilterState() == list.Filtering {
			break
		}

		switch {
		case viewkeys.MatchKey(msg, m.vk.SwitchLeft):
			m.focusedPanel = leftPanel
			return m, nil

		case viewkeys.MatchKey(msg, m.vk.SwitchRight):
			m.focusedPanel = rightPanel
			return m, nil

		case msg.String() == "enter":
			return m, m.enterDirectory()

		case msg.String() == "backspace":
			return m, m.goParentDir()

		case viewkeys.MatchKey(msg, m.vk.Upload):
			return m, m.uploadSelected()

		case viewkeys.MatchKey(msg, m.vk.Download):
			return m, m.downloadSelected()

		case viewkeys.MatchKey(msg, m.vk.Delete):
			return m, m.confirmDelete()

		case viewkeys.MatchKey(msg, m.vk.Mkdir):
			return m, m.mkdirCmd()

		case viewkeys.MatchKey(msg, m.vk.Rename):
			return m, m.confirmRename()

		case viewkeys.MatchKey(msg, m.vk.Chmod):
			return m, m.openChmod()

		case msg.String() == "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}

	case tea.PasteMsg:
		if m.namePromptActive {
			m.nameInput = inputpaste.TextInput(m.nameInput, msg)
			return m, nil
		}
		if m.chmodActive {
			m.chmodInput = inputpaste.TextInput(m.chmodInput, msg)
			return m, nil
		}
		if m.pathInputActive {
			if m.focusedPanel == leftPanel {
				m.localPathInput = inputpaste.TextInput(m.localPathInput, msg)
			} else {
				m.remotePathInput = inputpaste.TextInput(m.remotePathInput, msg)
			}
			m.updatePathTitles()
			return m, nil
		}

	case tea.MouseClickMsg:
		if m.namePromptActive {
			return m.handleNamePromptMouse(msg)
		}
		if m.chmodActive {
			if m2, cmd, done := m.handleChmodMouse(msg); done {
				return m2, cmd
			}
		}
		if m.localList.FilterState() == list.Filtering {
			break
		}
		if m.remoteList.FilterState() == list.Filtering {
			break
		}
		if m.width > 0 {
			if msg.X < m.width/2 {
				m.focusedPanel = leftPanel
			} else {
				m.focusedPanel = rightPanel
			}
		}
		if msg.Button == tea.MouseLeft && m.width > 0 && m.listInnerH > 0 {
			side, row, pathLine, ok := m.mouseTarget(msg.X, msg.Y)
			if ok {
				m.focusedPanel = side
				if pathLine {
					return m, m.beginPathInput(side)
				}
				if m.pathInputActive {
					m.cancelPathInput()
				}
				if row >= 0 {
					var lst list.Model
					if m.focusedPanel == leftPanel {
						lst = m.localList
					} else {
						lst = m.remoteList
					}
					vis := lst.VisibleItems()
					if len(vis) > 0 {
						start, end := lst.Paginator.GetSliceBounds(len(vis))
						onPage := end - start
						if row >= 0 && row < onPage {
							lst.Select(start + row)
							if m.focusedPanel == leftPanel {
								m.localList = lst
							} else {
								m.remoteList = lst
							}
							return m, nil
						}
					}
				}
			}
		}

	case tea.MouseWheelMsg:
		if m.namePromptActive {
			return m, nil
		}
		if m.chmodActive {
			return m, nil
		}
		if m.width > 0 {
			if msg.X < m.width/2 {
				m.focusedPanel = leftPanel
			} else {
				m.focusedPanel = rightPanel
			}
		}
	}

	if m.focusedPanel == leftPanel {
		var cmd tea.Cmd
		m.localList, cmd = m.localList.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.remoteList, cmd = m.remoteList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) mouseTarget(x, y int) (panelSide, int, bool, bool) {
	if m.width <= 0 || m.listInnerH <= 0 || x < 0 || y <= 0 {
		return leftPanel, -1, false, false
	}
	panelW := m.width / 2
	if panelW <= 0 {
		return leftPanel, -1, false, false
	}
	side := leftPanel
	localX := x
	if x >= panelW {
		side = rightPanel
		localX = x - panelW
	}
	if localX <= 0 || localX >= panelW-1 {
		return side, -1, false, false
	}
	contentY := y - 1
	if contentY < 0 || contentY >= m.listInnerH {
		return side, -1, false, false
	}
	const titleLines = 2
	if contentY < titleLines {
		return side, -1, true, true
	}
	return side, contentY - titleLines, false, true
}

func (m Model) handleChmodMouse(msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	ox, oy, ow, oh := m.chmodOverlayBounds()
	lx := msg.X - ox
	ly := msg.Y - oy
	if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
		m.chmodActive = false
		m.chmodPath = ""
		return m, nil, true
	}
	if msg.Button != tea.MouseLeft {
		return m, nil, true
	}
	if ly == 6 {
		return m, m.chmodInput.Focus(), true
	}
	if ly >= 8 {
		if lx < ow/2 {
			return m, m.chmodCmd(), true
		}
		m.chmodActive = false
		m.chmodPath = ""
		return m, nil, true
	}
	return m, nil, true
}

func (m Model) chmodOverlayBounds() (ox, oy, ow, oh int) {
	rendered := m.renderChmodOverlay()
	lines := strings.Split(rendered, "\n")
	oh = len(lines)
	for _, l := range lines {
		if w := lipgloss.Width(l); w > ow {
			ow = w
		}
	}
	layoutW := m.width
	if layoutW <= 0 {
		layoutW = 80
	}
	layoutH := m.height
	if layoutH <= 0 {
		layoutH = 24
	}
	ox = (layoutW - ow) / 2
	oy = (layoutH - oh) / 2
	return
}
