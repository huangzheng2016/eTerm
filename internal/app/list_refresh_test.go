package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type recordingTabModel struct {
	msgs []tea.Msg
}

func (m *recordingTabModel) Init() tea.Cmd { return nil }

func (m *recordingTabModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.msgs = append(m.msgs, msg)
	return m, nil
}

func (m *recordingTabModel) View() tea.View { return tea.NewView("") }

func sawRefreshConnectivity(m *recordingTabModel) bool {
	for _, msg := range m.msgs {
		if _, ok := msg.(types.RefreshConnectivityMsg); ok {
			return true
		}
	}
	return false
}

func sawRemoteDaemonRefresh(m *recordingTabModel) bool {
	for _, msg := range m.msgs {
		if _, ok := msg.(types.RemoteDaemonRefreshMsg); ok {
			return true
		}
	}
	return false
}

func TestSwitchingToListRefreshesConnectivity(t *testing.T) {
	list := &recordingTabModel{}
	ssh := &recordingTabModel{}
	a := App{
		viewState: MainView,
		tabs: []Tab{
			{Type: HomeTab, Title: "List", Model: list},
			{Type: SSHTab, Title: "ssh", Model: ssh},
		},
		activeTab: 1,
	}
	a.syncTabBar()

	out, _ := a.Update(types.SwitchTabMsg{Index: 0})
	a = out.(App)

	if a.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0", a.activeTab)
	}
	if !sawRefreshConnectivity(list) {
		t.Fatalf("list messages = %#v, want RefreshConnectivityMsg", list.msgs)
	}
}

func TestClickingActiveListRefreshesConnectivity(t *testing.T) {
	list := &recordingTabModel{}
	a := App{
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		viewState: MainView,
		width:     80,
		height:    24,
		tabs: []Tab{
			{Type: HomeTab, Title: "List", Model: list},
		},
		activeTab: 0,
	}
	a.syncTabBar()

	out, _ := a.Update(tea.MouseClickMsg(tea.Mouse{
		X:      3,
		Y:      0,
		Button: tea.MouseLeft,
	}))
	a = out.(App)

	if a.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0", a.activeTab)
	}
	if !sawRefreshConnectivity(list) {
		t.Fatalf("list messages = %#v, want RefreshConnectivityMsg", list.msgs)
	}
}

func TestClickingTabDoesNotBlockWhenTerminalResizeBlocks(t *testing.T) {
	release := make(chan struct{})
	terminal := sshview.New(&internalssh.InteractiveSession{
		Resize: func(rows, cols int) error {
			<-release
			return nil
		},
	}, "ssh", 0, viewkeys.SSHKeys{})
	defer func() {
		close(release)
		_ = terminal.Close()
	}()

	a := App{
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		viewState: MainView,
		width:     80,
		height:    24,
		tabs: []Tab{
			{Type: HomeTab, Title: "List", Model: &recordingTabModel{}},
			{Type: SSHTab, Title: "ssh", Model: terminal},
		},
		activeTab: 0,
	}
	a.syncTabBar()

	done := make(chan App, 1)
	go func() {
		out, _ := a.Update(tea.MouseClickMsg(tea.Mouse{
			X:      12,
			Y:      0,
			Button: tea.MouseLeft,
		}))
		done <- out.(App)
	}()

	select {
	case next := <-done:
		if next.activeTab != 1 {
			t.Fatalf("activeTab = %d, want 1", next.activeTab)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("tab click blocked on terminal resize")
	}
}

func TestRemoteDaemonRefreshForwardsToListTab(t *testing.T) {
	list := &recordingTabModel{}
	ssh := &recordingTabModel{}
	a := App{
		viewState: MainView,
		tabs: []Tab{
			{Type: HomeTab, Title: "List", Model: list},
			{Type: SSHTab, Title: "ssh", Model: ssh},
		},
		activeTab: 1,
	}

	out, _ := a.Update(types.RemoteDaemonRefreshMsg{})
	_ = out.(App)

	if !sawRemoteDaemonRefresh(list) {
		t.Fatalf("list messages = %#v, want RemoteDaemonRefreshMsg", list.msgs)
	}
	if sawRemoteDaemonRefresh(ssh) {
		t.Fatalf("ssh messages = %#v, want no RemoteDaemonRefreshMsg", ssh.msgs)
	}
}
