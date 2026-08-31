package app

import (
	"context"
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

func TestFindHostByName(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy", Tags: "web"})
	database.Create(&db.Host{Hostname: "plain.example.com", Port: 2222, Username: "root"})

	host, ok := findHostByName(database, "prod")
	if !ok || host.Hostname != "prod.internal" {
		t.Fatalf("alias lookup = %+v, %v", host, ok)
	}
	if _, ok := findHostByName(database, "root@plain.example.com"); !ok {
		t.Fatal("user@host lookup failed")
	}
	if _, ok := findHostByName(database, "nope"); ok {
		t.Fatal("unknown host resolved")
	}
}

func TestListHostsTool(t *testing.T) {
	database := aiTestDB(t)
	database.Create(&db.Host{Alias: "prod", Hostname: "prod.internal", Port: 22, Username: "deploy", Tags: "web"})
	exec := &aiExecutor{db: database, shared: &aiSharedState{}}

	hosts, err := exec.ListHosts(context.Background())
	if err != nil || len(hosts) != 1 {
		t.Fatalf("ListHosts = %+v, %v", hosts, err)
	}
	h := hosts[0]
	if h.Name != "prod" || h.Address != "prod.internal:22" || h.Tags != "web" {
		t.Fatalf("host = %+v", h)
	}
}

// serveOpenRequests emulates the UI side for the open_* ops: the open request
// is handled (and its connect cmd dropped); when landTab is set, a tab with
// the expected title materializes right after, like applyOpenSSHUITab would.
func serveOpenRequests(a App, ch <-chan aiToolRequest, landTab bool) {
	for req := range ch {
		_, cmd := a.handleAIToolRequest(req)
		_ = cmd
		if landTab && req.op == aiToolOpenSSH {
			host, _ := findHostByName(a.db, req.id)
			is := &internalssh.InteractiveSession{Done: make(chan error, 1)}
			sv := sshview.New(is, hostDisplayName(host), 0, BuildSSHKeys(DefaultKeyBindingConfig()))
			a.tabs = append(a.tabs, Tab{Type: SSHTab, Title: hostDisplayName(host), Model: sv})
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
	if r.err != nil || r.text != "prod" {
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
