package app

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

func TestOSCTitleUpdatesTabTitle(t *testing.T) {
	sv := sshview.New(nil, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	t.Cleanup(func() { _ = sv.Close() })
	a := App{tabs: []Tab{{Type: SSHTab, Title: "prod", Model: sv}}}

	updated, _ := a.Update(sshview.TitleMsg{StreamID: sv.StreamID(), Title: "user@host:~"})
	a = updated.(App)
	if a.tabs[0].Title != "user@host:~" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}

	// A repeat of the same title is a no-op.
	updated, _ = a.Update(sshview.TitleMsg{StreamID: sv.StreamID(), Title: "user@host:~"})
	a = updated.(App)
	if a.tabs[0].Title != "user@host:~" {
		t.Fatalf("title after repeat = %q", a.tabs[0].Title)
	}

	// Unknown stream ids match no tab.
	updated, _ = a.Update(sshview.TitleMsg{StreamID: 999999999, Title: "nope"})
	a = updated.(App)
	if a.tabs[0].Title != "user@host:~" {
		t.Fatalf("title after unknown stream = %q", a.tabs[0].Title)
	}
}

func TestOSCTitleManualRenamePrecedence(t *testing.T) {
	sv := sshview.New(nil, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	t.Cleanup(func() { _ = sv.Close() })
	a := App{tabs: []Tab{{Type: SSHTab, Title: "prod", Model: sv}}}

	a, _ = a.renameTab(tabRenameMsg{Index: 0, Title: "mine"})
	if !a.tabs[0].userRenamed {
		t.Fatal("rename did not set userRenamed")
	}

	updated, _ := a.Update(sshview.TitleMsg{StreamID: sv.StreamID(), Title: "user@host:~"})
	a = updated.(App)
	if a.tabs[0].Title != "mine" {
		t.Fatalf("renamed tab title = %q, want %q", a.tabs[0].Title, "mine")
	}
}
