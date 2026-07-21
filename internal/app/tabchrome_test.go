package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestMainTabChromeShowsReconnectBadgeOnToastLine(t *testing.T) {
	tab := sshview.New(nil, "[T]remote-work", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetRemoteReconnect(&types.RemoteReconnect{Peer: types.RemotePeer{ID: "p1"}, Tmux: true, Target: relay.TargetTmuxAttach, SessionID: "work"})
	_, _ = tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errConnectionResetForTest{}})

	a := App{
		tabs:      []Tab{{Type: SSHTab, Title: "[T]remote-work", Model: tab}},
		activeTab: 0,
		width:     80,
		toast:     components.NewToast(),
	}
	var cmd any
	a.toast, cmd = a.toast.Show("Remote reconnect - connect", components.ToastInfo, time.Second)
	if cmd == nil {
		t.Fatal("toast command should be set")
	}

	view := a.buildMainTabChrome(80)
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("tab chrome missing divider line:\n%s", view)
	}
	divider := lines[len(lines)-1]
	if !strings.Contains(divider, "Remote reconnect - connect") {
		t.Fatalf("divider missing toast:\n%s", divider)
	}
	if !strings.Contains(divider, "RECONNECTING (1/3)") {
		t.Fatalf("divider missing reconnect badge:\n%s", divider)
	}
	if strings.Contains(tab.View().Content, "RECONNECTING (1/3)") {
		t.Fatalf("ssh content should not render reconnect badge:\n%s", tab.View().Content)
	}
}

func TestBuildMainTabChromeUsesTabBarScrollState(t *testing.T) {
	a := App{
		tabs: []Tab{
			{Type: HomeTab, Title: "one"},
			{Type: SettingsTab, Title: "two"},
			{Type: SyncTab, Title: "three"},
			{Type: KeyTab, Title: "four"},
		},
		activeTab: 0,
		width:     22,
		toast:     components.NewToast(),
	}
	a.syncTabBar()
	a.tabBar = a.tabBar.ScrollRight()

	view := a.buildMainTabChrome(22)
	firstLine := strings.Split(view, "\n")[0]
	if strings.Contains(firstLine, "1:one") || !strings.Contains(firstLine, "2:two") {
		t.Fatalf("tab chrome ignored stored scroll state: %q", firstLine)
	}
}

func TestTabBarWheelPagingAndScrolledClickUseRenderedState(t *testing.T) {
	a := App{
		viewState: MainView,
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		tabs: []Tab{
			{Type: HomeTab, Title: "one"},
			{Type: SettingsTab, Title: "two"},
			{Type: SyncTab, Title: "three"},
			{Type: KeyTab, Title: "four"},
		},
		activeTab: 0,
		width:     22,
		height:    20,
		toast:     components.NewToast(),
	}
	a.syncTabBar()

	for range 2 {
		next, _ := a.Update(tea.MouseWheelMsg(tea.Mouse{X: 4, Y: 0, Button: tea.MouseWheelDown}))
		a = next.(App)
	}
	firstLine := strings.Split(a.buildMainTabChrome(22), "\n")[0]
	if strings.Contains(firstLine, "2:two") || !strings.Contains(firstLine, "3:three") {
		t.Fatalf("repeated wheel did not page from persistent state: %q", firstLine)
	}

	next, _ := a.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: 0, Button: tea.MouseLeft}))
	a = next.(App)
	if a.activeTab != 2 {
		t.Fatalf("scrolled visible tab click activated %d, want 2", a.activeTab)
	}
}

func TestTabPageKeysScrollWithoutChangingActiveTab(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	a := App{
		viewState: MainView,
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		tabs: []Tab{
			{Type: HomeTab, Title: "one"},
			{Type: SettingsTab, Title: "two"},
			{Type: SyncTab, Title: "three"},
			{Type: KeyTab, Title: "four"},
		},
		activeTab: 0,
		width:     22,
		height:    20,
		toast:     components.NewToast(),
		keyMap:    BuildKeyMap(cfg),
		kbConfig:  cfg,
	}
	a.syncTabBar()

	next, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModAlt | tea.ModShift}))
	a = next.(App)
	if a.activeTab != 0 {
		t.Fatalf("right page changed activeTab to %d", a.activeTab)
	}
	firstLine := strings.Split(a.buildMainTabChrome(22), "\n")[0]
	if strings.Contains(firstLine, "1:one") || !strings.Contains(firstLine, "2:two") {
		t.Fatalf("right page did not persist scroll: %q", firstLine)
	}

	next, _ = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt | tea.ModShift}))
	a = next.(App)
	if a.activeTab != 0 {
		t.Fatalf("left page changed activeTab to %d", a.activeTab)
	}
	firstLine = strings.Split(a.buildMainTabChrome(22), "\n")[0]
	if !strings.Contains(firstLine, "1:one") {
		t.Fatalf("left page did not persist scroll: %q", firstLine)
	}
}

func TestAltShiftSDoesNotCycleSSHTabs(t *testing.T) {
	cfg := defaultKeyBindingConfig("windows")
	a := App{
		viewState: MainView,
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		tabs:      []Tab{{Type: SSHTab}, {Type: SSHTab}},
		activeTab: 0,
		keyMap:    BuildKeyMap(cfg),
		kbConfig:  cfg,
	}

	next, _ := a.Update(tea.KeyPressMsg(tea.Key{Code: 's', ShiftedCode: 'S', Mod: tea.ModAlt | tea.ModShift}))
	if next.(App).activeTab != 0 {
		t.Fatalf("active tab = %d, want 0", next.(App).activeTab)
	}
}

type errConnectionResetForTest struct{}

func (errConnectionResetForTest) Error() string { return "read: connection reset by peer" }
