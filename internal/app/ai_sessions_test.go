package app

import (
	"testing"

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
