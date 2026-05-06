package sftpview

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sort"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/sftp"
)

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

func (m *Model) openChmod() tea.Cmd {
	if m.focusedPanel != rightPanel {
		return nil
	}
	item := m.remoteList.SelectedItem()
	if item == nil {
		return nil
	}
	fi := item.(fileItem)
	m.chmodPath = remoteJoin(m.remotePath, fi.info.Name)
	m.chmodInput.SetValue(fmt.Sprintf("%04o", fi.info.Mode.Perm()))
	m.chmodActive = true
	return m.chmodInput.Focus()
}

func (m *Model) chmodCmd() tea.Cmd {
	modeStr := m.chmodInput.Value()
	if len(modeStr) != 4 {
		m.err = "Mode must be 4 octal digits"
		return nil
	}
	perm, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil || perm > 0o777 {
		m.err = "Invalid octal mode"
		return nil
	}
	path := m.chmodPath
	client := m.sftpClient
	m.chmodActive = false
	m.chmodPath = ""
	return func() tea.Msg {
		err := client.Chmod(path, os.FileMode(perm))
		if err != nil {
			return transferCompleteMsg{err: err}
		}
		return transferCompleteMsg{}
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
