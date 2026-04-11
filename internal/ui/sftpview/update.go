package sftpview

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/sftp"
	"github.com/eterm/eterm/internal/types"
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
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		items := filesToItems(msg.files)
		cmd := m.localList.SetItems(items)
		m.localList.Title = "Local: " + m.localPath
		return m, cmd

	case remoteFilesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		items := filesToItems(msg.files)
		cmd := m.remoteList.SetItems(items)
		m.remoteList.Title = "Remote: " + m.remotePath
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

		if m.focusedPanel == leftPanel && m.localList.FilterState() == list.Filtering {
			break
		}
		if m.focusedPanel == rightPanel && m.remoteList.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "left", "h":
			m.focusedPanel = leftPanel
			return m, nil

		case "right", "l":
			m.focusedPanel = rightPanel
			return m, nil

		case "enter":
			return m, m.enterDirectory()

		case "backspace":
			return m, m.goParentDir()

		case "u":
			return m, m.uploadSelected()

		case "d":
			return m, m.downloadSelected()

		case "delete", "x":
			return m, m.confirmDelete()

		case "m":
			return m, m.mkdirCmd()

		case "r":
			return m, m.confirmRename()

		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}

	case tea.MouseClickMsg:
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
			if msg.Y >= 1 && msg.Y <= m.listInnerH {
				innerY := msg.Y - 1
				const titleLines = 1
				if innerY >= titleLines {
					row := innerY - titleLines
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

func (m Model) loadLocalFiles() tea.Cmd {
	path := m.localPath
	return func() tea.Msg {
		entries, err := os.ReadDir(path)
		if err != nil {
			return localFilesMsg{err: err}
		}
		files := make([]sftp.FileInfo, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, sftp.FileInfo{
				Name:    e.Name(),
				Size:    info.Size(),
				IsDir:   e.IsDir(),
				ModTime: info.ModTime(),
				Mode:    info.Mode(),
			})
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].IsDir != files[j].IsDir {
				return files[i].IsDir
			}
			return files[i].Name < files[j].Name
		})
		return localFilesMsg{files: files}
	}
}

func (m Model) loadRemoteFiles() tea.Cmd {
	client := m.sftpClient
	path := m.remotePath
	return func() tea.Msg {
		files, err := client.List(path)
		if err != nil {
			return remoteFilesMsg{err: err}
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].IsDir != files[j].IsDir {
				return files[i].IsDir
			}
			return files[i].Name < files[j].Name
		})
		return remoteFilesMsg{files: files}
	}
}

func (m *Model) enterDirectory() tea.Cmd {
	if m.focusedPanel == leftPanel {
		item := m.localList.SelectedItem()
		if item == nil {
			return nil
		}
		fi := item.(fileItem)
		if !fi.info.IsDir {
			return nil
		}
		m.localPath = filepath.Join(m.localPath, fi.info.Name)
		return m.loadLocalFiles()
	}
	item := m.remoteList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	if !fi.info.IsDir {
		return nil
	}
	m.remotePath = remoteJoin(m.remotePath, fi.info.Name)
	return m.loadRemoteFiles()
}

func (m *Model) goParentDir() tea.Cmd {
	if m.focusedPanel == leftPanel {
		parent := filepath.Dir(m.localPath)
		if parent != m.localPath {
			m.localPath = parent
			return m.loadLocalFiles()
		}
		return nil
	}
	if m.remotePath == "/" {
		return nil
	}
	idx := len(m.remotePath) - 1
	for idx > 0 && m.remotePath[idx] != '/' {
		idx--
	}
	if idx == 0 {
		m.remotePath = "/"
	} else {
		m.remotePath = m.remotePath[:idx]
	}
	return m.loadRemoteFiles()
}

func (m *Model) uploadSelected() tea.Cmd {
	item := m.localList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	localPath := filepath.Join(m.localPath, fi.info.Name)
	remotePath := remoteJoin(m.remotePath, fi.info.Name)
	client := m.sftpClient
	ch := m.progressCh
	m.transferring = true

	cb := func(p sftp.TransferProgress) {
		select {
		case ch <- p:
		default:
		}
	}

	if fi.info.IsDir {
		return tea.Batch(func() tea.Msg {
			err := sftp.UploadDir(client, localPath, remotePath, cb)
			return transferCompleteMsg{err: err}
		}, waitProgress(ch))
	}

	return tea.Batch(func() tea.Msg {
		err := sftp.Upload(client, localPath, remotePath, cb)
		return transferCompleteMsg{err: err}
	}, waitProgress(ch))
}

func (m *Model) downloadSelected() tea.Cmd {
	item := m.remoteList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	remotePath := remoteJoin(m.remotePath, fi.info.Name)
	localPath := filepath.Join(m.localPath, fi.info.Name)
	client := m.sftpClient
	ch := m.progressCh
	m.transferring = true

	cb := func(p sftp.TransferProgress) {
		select {
		case ch <- p:
		default:
		}
	}

	if fi.info.IsDir {
		return tea.Batch(func() tea.Msg {
			err := sftp.DownloadDir(client, remotePath, localPath, cb)
			return transferCompleteMsg{err: err}
		}, waitProgress(ch))
	}

	return tea.Batch(func() tea.Msg {
		err := sftp.Download(client, remotePath, localPath, cb)
		return transferCompleteMsg{err: err}
	}, waitProgress(ch))
}

func (m Model) deleteSelected() tea.Cmd {
	if m.focusedPanel == leftPanel {
		item := m.localList.SelectedItem()
		if item == nil {
			return nil
		}
		fi := item.(fileItem)
		path := filepath.Join(m.localPath, fi.info.Name)
		return func() tea.Msg {
			var err error
			if fi.info.IsDir {
				err = os.RemoveAll(path)
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				return transferCompleteMsg{err: err}
			}
			return transferCompleteMsg{}
		}
	}

	item := m.remoteList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	remotePath := remoteJoin(m.remotePath, fi.info.Name)
	client := m.sftpClient
	return func() tea.Msg {
		err := client.Remove(remotePath)
		if err != nil {
			return transferCompleteMsg{err: err}
		}
		return transferCompleteMsg{}
	}
}

func (m Model) mkdirCmd() tea.Cmd {
	if m.focusedPanel == leftPanel {
		path := filepath.Join(m.localPath, "new_folder")
		return func() tea.Msg {
			err := os.MkdirAll(path, 0o755)
			return transferCompleteMsg{err: err}
		}
	}
	remotePath := remoteJoin(m.remotePath, "new_folder")
	client := m.sftpClient
	return func() tea.Msg {
		err := client.Mkdir(remotePath)
		return transferCompleteMsg{err: err}
	}
}

func (m Model) renameCmd() tea.Cmd {
	if m.focusedPanel == leftPanel {
		item := m.localList.SelectedItem()
		if item == nil {
			return nil
		}
		fi := item.(fileItem)
		oldPath := filepath.Join(m.localPath, fi.info.Name)
		newPath := filepath.Join(m.localPath, fi.info.Name+"_renamed")
		return func() tea.Msg {
			err := os.Rename(oldPath, newPath)
			return transferCompleteMsg{err: err}
		}
	}

	item := m.remoteList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	oldPath := remoteJoin(m.remotePath, fi.info.Name)
	newPath := remoteJoin(m.remotePath, fi.info.Name+"_renamed")
	client := m.sftpClient
	return func() tea.Msg {
		err := client.Rename(oldPath, newPath)
		return transferCompleteMsg{err: err}
	}
}

func filesToItems(files []sftp.FileInfo) []list.Item {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = fileItem{info: f}
	}
	return items
}

func waitProgress(ch <-chan sftp.TransferProgress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return transferProgressMsg{progress: p}
	}
}

// remoteJoin joins a remote directory path with a file name, avoiding double slashes.
func remoteJoin(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

func (m *Model) confirmDelete() tea.Cmd {
	var name string
	if m.focusedPanel == leftPanel {
		item := m.localList.SelectedItem()
		if item == nil {
			return nil
		}
		name = item.(fileItem).info.Name
	} else {
		item := m.remoteList.SelectedItem()
		if item == nil {
			return nil
		}
		name = item.(fileItem).info.Name
	}
	m.confirmMsg = fmt.Sprintf("Delete %q?", name)
	m.pendingAction = func() tea.Cmd { return m.deleteSelected() }
	return nil
}

func (m *Model) confirmRename() tea.Cmd {
	var name string
	if m.focusedPanel == leftPanel {
		item := m.localList.SelectedItem()
		if item == nil {
			return nil
		}
		name = item.(fileItem).info.Name
	} else {
		item := m.remoteList.SelectedItem()
		if item == nil {
			return nil
		}
		name = item.(fileItem).info.Name
	}
	m.confirmMsg = fmt.Sprintf("Rename %q to %q?", name, name+"_renamed")
	m.pendingAction = func() tea.Cmd { return m.renameCmd() }
	return nil
}
