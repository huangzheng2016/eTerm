package app

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

// historyAgent is a fakeAgent with a stateful history for session tests.
type historyAgent struct {
	fakeAgent
	history []byte
	undone  bool
}

func (a *historyAgent) ExportHistory(capBytes int) ([]byte, error) { return a.history, nil }
func (a *historyAgent) ImportHistory(data []byte) error {
	a.history = data
	return nil
}
func (a *historyAgent) UndoLastTurn() { a.undone = true }

func testSessionDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&aiSession{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func testSessionBridge(database *gorm.DB) *aiBridge {
	store := &ai.Store{ActiveProvider: "p", ActiveModel: "m"}
	return &aiBridge{store: store, db: database}
}

func TestBridgeSessionSaveListLoad(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	agent := &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.agent = agent

	bridge.SaveSession("s1", "hi there", "")
	list := bridge.Sessions()
	if len(list) != 1 {
		t.Fatalf("got %d sessions, want 1", len(list))
	}
	e := list[0]
	if e.ID != "s1" || e.Title != "hi there" || e.Provider != "p" || e.Model != "m" {
		t.Fatalf("entry = %+v", e)
	}

	agent.history = nil // simulate a fresh agent
	data, ok := bridge.LoadSession("s1")
	if !ok {
		t.Fatal("load failed")
	}
	if string(data) != string(agent.history) {
		t.Fatalf("history not imported into agent: %q vs %q", data, agent.history)
	}
	if _, ok := bridge.LoadSession("missing"); ok {
		t.Fatal("unknown session must not load")
	}

	// Update keeps the row and refreshes the title.
	agent.history = []byte(`[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"}]`)
	bridge.SaveSession("s1", "", "")
	if list = bridge.Sessions(); len(list) != 1 || list[0].Title != "hi there" {
		t.Fatalf("update duplicated or renamed the row: %+v", list)
	}
	var row aiSession
	if err := bridge.db.Where("id = ?", "s1").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if len(row.History) == 0 {
		t.Fatal("history not updated")
	}
}

func TestBridgeSaveSessionSkipsEmptyHistory(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	agent := &historyAgent{}
	bridge.agent = agent
	bridge.SaveSession("s1", "t", "")
	if n := len(bridge.Sessions()); n != 0 {
		t.Fatalf("empty history created a row: %d", n)
	}

	// An existing row is still updated (e.g. emptied by /undo).
	agent.history = []byte(`[{"role":"user","content":"hi"}]`)
	bridge.SaveSession("s2", "t", "")
	agent.history = nil
	bridge.SaveSession("s2", "", "")
	var row aiSession
	if err := bridge.db.Where("id = ?", "s2").First(&row).Error; err != nil {
		t.Fatal("existing row must be updated even with empty history")
	}
	if len(row.History) != 0 {
		t.Fatalf("history not emptied: %q", row.History)
	}
}

func TestBridgeForkOfStoredOnCreate(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	bridge.agent = &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.SaveSession("fork", "hi", "parent")
	var row aiSession
	if err := bridge.db.Where("id = ?", "fork").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ForkOf != "parent" {
		t.Fatalf("fork_of = %q, want parent", row.ForkOf)
	}
}

func TestBridgePendingHistoryStashAndApply(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	bridge.agent = &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.SaveSession("s1", "hi", "")
	bridge.agent = nil

	if _, ok := bridge.LoadSession("s1"); !ok {
		t.Fatal("load failed")
	}
	if bridge.pendingHistory == nil {
		t.Fatal("history not stashed without an agent")
	}
	p := &ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"}
	agent, err := bridge.agentFor(p, "m", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := agent.ExportHistory(0)
	if err != nil || len(data) == 0 {
		t.Fatalf("stashed history not applied to the new agent: %q, %v", data, err)
	}
	if bridge.pendingHistory != nil {
		t.Fatal("pending history not consumed")
	}
}

func TestBridgeUndoLastTurn(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	agent := &historyAgent{}
	bridge.agent = agent
	bridge.UndoLastTurn()
	if !agent.undone {
		t.Fatal("agent undo not called")
	}

	// Without an agent, the stashed history is truncated instead.
	bridge.agent = nil
	bridge.pendingHistory = []byte(`[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]`)
	bridge.UndoLastTurn()
	want := `[{"role":"user","content":"a"},{"role":"assistant","content":"b"}]`
	if string(bridge.pendingHistory) != want {
		t.Fatalf("pending history = %q, want %q", bridge.pendingHistory, want)
	}
}

func TestBridgeResetHistoryClearsPending(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	bridge.pendingHistory = []byte("x")
	bridge.ResetHistory()
	if bridge.pendingHistory != nil {
		t.Fatal("pending history not cleared")
	}
}

func TestAgentForCarriesHistoryOnSwitch(t *testing.T) {
	bridge := testSessionBridge(testSessionDB(t))
	bridge.agent = &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.agentKey = "old\x00model\x00false"
	p := &ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"}
	agent, err := bridge.agentFor(p, "m", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := agent.ExportHistory(0)
	if err != nil || len(data) == 0 {
		t.Fatal("history not carried over to the replacement agent")
	}
}

func TestNewAIBridgeMigratesSessions(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	bridge := newAIBridge(database, security.NewMasterKeyManager(nil, nil, 0), nil)
	bridge.agent = &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.SaveSession("s1", "hi", "")
	if n := len(bridge.Sessions()); n != 1 {
		t.Fatalf("got %d sessions, want 1 (auto-migrated table)", n)
	}
}

func testCronBridge(t *testing.T) *aiBridge {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return newAIBridge(database, security.NewMasterKeyManager(nil, nil, 0), nil)
}

func TestBridgeCronPersistenceRoundTrip(t *testing.T) {
	bridge := testCronBridge(t)
	bridge.setCronSession("s1")
	job, err := bridge.cron.Create("watch the build", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if job.SessionID != "s1" {
		t.Fatalf("job session = %q", job.SessionID)
	}

	// Restart: a fresh bridge on the same db reloads the session's jobs.
	bridge2 := testCronBridge(t)
	bridge2.db = bridge.db
	bridge2.setCronSession("s1")
	jobs := bridge2.cron.List()
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Prompt != "watch the build" || jobs[0].IntervalMinutes != 5 {
		t.Fatalf("reloaded jobs: %+v", jobs)
	}
	// Other sessions see nothing.
	bridge2.setCronSession("s2")
	if n := len(bridge2.cron.List()); n != 0 {
		t.Fatalf("jobs leaked into s2: %d", n)
	}
}

func TestBridgeCronAdoptsUnsavedJobs(t *testing.T) {
	bridge := testCronBridge(t)
	// Job created during a conversation that has no session id yet.
	if _, err := bridge.cron.Create("check back", 5, 0); err != nil {
		t.Fatal(err)
	}
	bridge.agent = &historyAgent{history: []byte(`[{"role":"user","content":"hi"}]`)}
	bridge.SaveSession("s1", "hi", "")
	jobs, err := bridge.LoadCronJobs("s1")
	if err != nil || len(jobs) != 1 || jobs[0].Prompt != "check back" {
		t.Fatalf("jobs not adopted by first save: %+v %v", jobs, err)
	}
	if n := len(bridge.cron.List()); n != 1 {
		t.Fatalf("adopted job not loaded into the scheduler: %d", n)
	}
}

func TestBridgeCronOverdueFiresOnLoad(t *testing.T) {
	bridge := testCronBridge(t)
	fireCh := make(chan aiToolRequest, 1)
	bridge.fireCh = fireCh
	past := time.Now().Add(-time.Hour)
	if err := bridge.db.Create(&aiCronJob{
		ID: "job1", SessionID: "s1", Prompt: "report the tab state",
		IntervalMinutes: 5, NextFireAt: past, CreatedAt: past,
	}).Error; err != nil {
		t.Fatal(err)
	}
	bridge.setCronSession("s1")
	select {
	case req := <-fireCh:
		if req.op != aiToolCronFire {
			t.Fatalf("op = %v", req.op)
		}
		if !strings.Contains(req.arg, "report the tab state") || !strings.Contains(req.arg, "job1") || !strings.Contains(req.arg, "coalesced") {
			t.Fatalf("wake text: %q", req.arg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("overdue job did not fire after session load")
	}
	// The recurring job was rescheduled, not deleted.
	jobs := bridge.cron.List()
	if len(jobs) != 1 || !jobs[0].NextFireAt.After(time.Now()) {
		t.Fatalf("job not rescheduled: %+v", jobs)
	}
}
