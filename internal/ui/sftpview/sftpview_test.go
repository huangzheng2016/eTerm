package sftpview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/sftp"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestConfirmRenameStartsNamePromptWithSelectedName(t *testing.T) {
	m := New(nil, "host", viewkeys.SFTPKeys{})
	_ = m.remoteList.SetItems([]list.Item{fileItem{info: sftp.FileInfo{Name: "old.txt"}}})
	m.focusedPanel = rightPanel

	cmd := m.confirmRename()

	if cmd == nil {
		t.Fatal("expected prompt focus command")
	}
	if !m.namePromptActive || m.namePromptKind != "rename" {
		t.Fatalf("got active=%v kind=%q, want active rename prompt", m.namePromptActive, m.namePromptKind)
	}
	if got := m.nameInput.Value(); got != "old.txt" {
		t.Fatalf("got %q want old.txt", got)
	}
}

func TestDownloadSelectedConfirmsWhenLocalTargetExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "remote.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New(nil, "host", viewkeys.SFTPKeys{})
	m.localPath = dir
	m.remotePath = "/tmp"
	_ = m.remoteList.SetItems([]list.Item{fileItem{info: sftp.FileInfo{Name: "remote.txt"}}})
	m.focusedPanel = rightPanel

	cmd := m.downloadSelected()

	if cmd != nil {
		t.Fatal("expected confirmation before download command")
	}
	if m.confirmMsg == "" || m.pendingAction == nil {
		t.Fatal("expected overwrite confirmation")
	}
}

func TestMkdirStartsNamePromptWithDefaultName(t *testing.T) {
	m := New(nil, "host", viewkeys.SFTPKeys{})

	cmd := m.mkdirCmd()

	if cmd == nil {
		t.Fatal("expected prompt focus command")
	}
	if !m.namePromptActive || m.namePromptKind != "mkdir" {
		t.Fatalf("got active=%v kind=%q, want active mkdir prompt", m.namePromptActive, m.namePromptKind)
	}
	if got := m.nameInput.Value(); got != "new_folder" {
		t.Fatalf("got %q want new_folder", got)
	}
}

func TestNamePromptMouseOutsideCancels(t *testing.T) {
	m := New(nil, "host", viewkeys.SFTPKeys{})
	m.SetSize(80, 24)
	_ = m.mkdirCmd()

	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      0,
		Y:      0,
		Button: tea.MouseLeft,
	}))
	updated := next.(Model)

	if updated.namePromptActive {
		t.Fatal("expected outside click to cancel name prompt")
	}
}

func TestFooterAndHelpStaySingleLineWithinWidth(t *testing.T) {
	m := New(nil, "host", viewkeys.SFTPKeys{
		SwitchLeft:  []string{"very-long-left-binding"},
		SwitchRight: []string{"very-long-right-binding"},
		Upload:      []string{"very-long-upload-binding"},
		Download:    []string{"very-long-download-binding"},
		Delete:      []string{"very-long-delete-binding"},
		Mkdir:       []string{"very-long-mkdir-binding"},
		Rename:      []string{"very-long-rename-binding"},
		Chmod:       []string{"very-long-chmod-binding"},
	})
	m.SetSize(30, 12)
	m.progress.CurrentFile = strings.Repeat("remote-file-", 20)
	m.progress.TotalBytes = 100
	m.progress.TransferredBytes = 50
	m.transferring = true

	for _, line := range []string{m.composeFooter(), m.composeHelpLine()} {
		if strings.Contains(line, "\n") {
			t.Fatalf("expected single line, got %q", line)
		}
		if lipgloss.Width(line) > 30 {
			t.Fatalf("line width %d exceeds 30: %q", lipgloss.Width(line), line)
		}
	}
}
