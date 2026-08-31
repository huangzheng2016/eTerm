package app

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

func aiTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestFindHostsByName(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy", Tags: "web"})
	database.Create(&db.Host{Hostname: "plain.example.com", Port: 2222, Username: "root"})
	database.Create(&db.Host{Hostname: "plain.example.com", Port: 22, Username: "root"})

	hosts := findHostsByName(database, "prod")
	if len(hosts) != 1 || hosts[0].Hostname != "prod.internal" {
		t.Fatalf("alias lookup = %+v", hosts)
	}
	if got := findHostsByName(database, "root@plain.example.com"); len(got) != 2 {
		t.Fatalf("duplicate display name: got %d matches, want 2", len(got))
	}
	if got := findHostsByName(database, "nope"); len(got) != 0 {
		t.Fatalf("unknown host resolved: %+v", got)
	}
}

func TestListHostsTool(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy", Tags: "web"})
	var host db.Host
	database.First(&host)
	exec := &aiExecutor{db: database, shared: &aiSharedState{}}

	hosts, err := exec.ListHosts(context.Background())
	if err != nil || len(hosts) != 1 {
		t.Fatalf("ListHosts = %+v, %v", hosts, err)
	}
	h := hosts[0]
	if h.Name != "prod" || h.Address != "prod.internal:22" || h.Tags != "web" || h.ID != host.ID {
		t.Fatalf("host = %+v", h)
	}
}

// serveOpenRequests emulates the UI side for the open_* ops: the open request
// is handled (and its connect cmd dropped); when landTab is set, a tab for the
// host materializes right after, like applyOpenSSHUITab would. The tab title
// is deliberately NOT the host alias (a remote OSC 0/2 can retitle the tab
// before the poll sees it); matching must rely on the host id.
func serveOpenRequests(a App, ch <-chan aiToolRequest, landTab bool) {
	for req := range ch {
		_, cmd := a.handleAIToolRequest(req)
		_ = cmd
		if landTab && req.op == aiToolOpenSSH {
			hosts := findHostsByName(a.db, req.id)
			if len(hosts) != 1 {
				continue
			}
			is := &internalssh.InteractiveSession{Done: make(chan error, 1)}
			sv := sshview.New(is, "renamed-by-osc", hosts[0].ID, BuildSSHKeys(DefaultKeyBindingConfig()))
			a.tabs = append(a.tabs, Tab{Type: SSHTab, Title: "renamed-by-osc", Model: sv})
		}
	}
}

func TestOpenSSHLandsNewTabID(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy"})
	ch := make(chan aiToolRequest, 16)
	a := App{db: database, aiShared: &aiSharedState{}}
	exec := &aiExecutor{db: database, reqCh: ch, shared: a.aiShared}
	go serveOpenRequests(a, ch, true)
	defer close(ch)

	// The tab lands already retitled; the host-id match must still find it.
	id, err := exec.OpenSSH(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || strings.HasPrefix(id, "tab-") {
		t.Fatalf("tab id = %q", id)
	}

	if _, err := exec.OpenSSH(context.Background(), "nope"); err == nil || !strings.Contains(err.Error(), "unknown host") {
		t.Fatalf("unknown host err = %v", err)
	}
}

func TestOpenSSHAmbiguousHostName(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Hostname: "dup.example.com", Port: 22, Username: "root"})
	database.Create(&db.Host{Hostname: "dup.example.com", Port: 2222, Username: "root"})
	ch := make(chan aiToolRequest, 16)
	a := App{db: database, aiShared: &aiSharedState{}}
	exec := &aiExecutor{db: database, reqCh: ch, shared: a.aiShared}
	go serveOpenRequests(a, ch, true)
	defer close(ch)

	_, err := exec.OpenSSH(context.Background(), "root@dup.example.com")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "dup.example.com:2222") {
		t.Fatalf("error must list candidates: %v", err)
	}
}

func TestOpenSSHTimeoutWhenNoTabLands(t *testing.T) {
	oldTimeout, oldInterval := aiOpenSSHTimeout, aiOpenPollInterval
	aiOpenSSHTimeout, aiOpenPollInterval = 300*time.Millisecond, 20*time.Millisecond
	defer func() { aiOpenSSHTimeout, aiOpenPollInterval = oldTimeout, oldInterval }()

	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy"})
	ch := make(chan aiToolRequest, 16)
	a := App{db: database, aiShared: &aiSharedState{}}
	exec := &aiExecutor{db: database, reqCh: ch, shared: a.aiShared}
	go serveOpenRequests(a, ch, false)
	defer close(ch)

	_, err := exec.OpenSSH(context.Background(), "prod")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindFreshAITab(t *testing.T) {
	streamID := func(tab Tab) string {
		return strconv.FormatUint(tab.Model.(*sshview.Model).StreamID(), 10)
	}
	newTab := func(typ TabType, title string, hostID uint, tmuxSession string) Tab {
		is := &internalssh.InteractiveSession{Done: make(chan error, 1)}
		sv := sshview.New(is, title, hostID, BuildSSHKeys(DefaultKeyBindingConfig()))
		return Tab{Type: typ, Title: title, Model: sv, TmuxSession: tmuxSession}
	}
	sshTab := newTab(SSHTab, "whatever", 42, "")
	tmuxTab := newTab(LocalTab, "whatever", 0, "work")
	localTab := newTab(LocalTab, "whatever", 0, "")
	a := App{tabs: []Tab{sshTab, tmuxTab, localTab}}

	if got := a.findFreshAITab("ssh", "42", nil); got != streamID(sshTab) {
		t.Fatalf("ssh match = %q, want %s", got, streamID(sshTab))
	}
	if got := a.findFreshAITab("ssh", "7", nil); got != "" {
		t.Fatalf("wrong host matched: %q", got)
	}
	if got := a.findFreshAITab("ssh", "42", []string{streamID(sshTab)}); got != "" {
		t.Fatal("before-set id not excluded")
	}
	if got := a.findFreshAITab("tmux", "work", nil); got != streamID(tmuxTab) {
		t.Fatalf("tmux match = %q", got)
	}
	if got := a.findFreshAITab("tmux", "other", nil); got != "" {
		t.Fatalf("wrong session matched: %q", got)
	}
	// The tmux tab is LocalTab too; the local matcher must skip it.
	if got := a.findFreshAITab("local", "", nil); got != streamID(localTab) {
		t.Fatalf("local match = %q, want %s", got, streamID(localTab))
	}
}

func TestToggleLocalToolsPersists(t *testing.T) {
	database := aiTestDB(t)
	if loadAILocalTools(database) {
		t.Fatal("default must be off")
	}
	bridge := &aiBridge{db: database}
	if !bridge.ToggleLocalTools() {
		t.Fatal("first toggle must turn on")
	}
	if !loadAILocalTools(database) {
		t.Fatal("toggle not persisted")
	}
	if bridge.ToggleLocalTools() {
		t.Fatal("second toggle must turn off")
	}
	if loadAILocalTools(database) {
		t.Fatal("off state not persisted")
	}
}

// The returned connect cmd must be an SSHConnectMsg for the resolved host,
// i.e. the same message the command palette emits.
func TestOpenSSHRequestEmitsPaletteConnectMsg(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy"})
	var host db.Host
	database.First(&host)
	a := App{db: database, aiShared: &aiSharedState{}}
	req := aiToolRequest{op: aiToolOpenSSH, id: "prod", resp: make(chan aiToolResult, 1)}
	_, cmd := a.handleAIToolRequest(req)
	r := <-req.resp
	if r.err != nil || r.text == "" {
		t.Fatalf("result = %+v", r)
	}
	if cmd == nil {
		t.Fatal("no connect cmd returned")
	}
	connect, ok := cmd().(types.SSHConnectMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want types.SSHConnectMsg", cmd())
	}
	if connect.HostID != host.ID {
		t.Fatalf("HostID = %d, want %d", connect.HostID, host.ID)
	}
}
