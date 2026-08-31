package app

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"gorm.io/gorm"
)

// fakeAgent mimics ai.Agent: Run streams until ctx ends (then closes, like
// the real runner), and Clear blocks while a run holds the agent mutex.
type fakeAgent struct {
	clearRelease   chan struct{}
	queued         []string
	snaps          []ai.TaskSnapshot
	cancelledTasks []string
}

func (f *fakeAgent) Run(ctx context.Context, input string) <-chan ai.Event {
	ch := make(chan ai.Event)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch
}

func (f *fakeAgent) Clear() {
	if f.clearRelease != nil {
		<-f.clearRelease
	}
}

func (f *fakeAgent) ExportHistory(capBytes int) ([]byte, error) { return nil, nil }
func (f *fakeAgent) ImportHistory(data []byte) error            { return nil }
func (f *fakeAgent) UndoLastTurn()                              {}
func (f *fakeAgent) Enqueue(text string)                        { f.queued = append(f.queued, text) }
func (f *fakeAgent) ClearQueue()                                { f.queued = nil }
func (f *fakeAgent) TaskSnapshots() []ai.TaskSnapshot           { return f.snaps }
func (f *fakeAgent) CancelTask(id string) bool {
	f.cancelledTasks = append(f.cancelledTasks, id)
	return true
}

func TestCtrlLReturnsPromptlyMidRun(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	bridge := &aiBridge{}
	bridge.agent = &fakeAgent{clearRelease: release}
	a := App{
		aiView:    aiview.New(bridge, bridge, bridge),
		aiBridge:  bridge,
		aiVisible: true,
	}
	done := make(chan struct{})
	go func() {
		a.Update(tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ctrl+l blocked while the agent was busy")
	}
}

func TestBridgeCancelRun(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"})
	if err := store.SetActive("p", "m"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store}
	bridge.agent = &fakeAgent{}
	bridge.agentKey = "p\x00m\x00false"

	out, err := bridge.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	bridge.CancelRun()
	select {
	case _, ok := <-out:
		if ok {
			for range out {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event pump did not stop after CancelRun")
	}
	bridge.CancelRun() // no panic when idle
}

func TestBridgeEnqueueRoutesToAgent(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"})
	if err := store.SetActive("p", "m"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store}
	agent := &fakeAgent{}
	bridge.agent = agent
	bridge.agentKey = "p\x00m\x00false"

	if err := bridge.Enqueue("too early"); err == nil {
		t.Fatal("Enqueue without a run must fail")
	}
	if len(agent.queued) != 0 {
		t.Fatalf("failed Enqueue must not queue: %v", agent.queued)
	}
	if _, err := bridge.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Enqueue("steer this"); err != nil {
		t.Fatal(err)
	}
	if len(agent.queued) != 1 || agent.queued[0] != "steer this" {
		t.Fatalf("queued: %v", agent.queued)
	}
	bridge.CancelRun()
	if len(agent.queued) != 0 {
		t.Fatalf("CancelRun must clear the queue, got %v", agent.queued)
	}
}

type usageAgent struct{ fakeAgent }

func (usageAgent) Usage() (int, int) { return 100, 1000 }

func TestBridgeContextUsage(t *testing.T) {
	bridge := &aiBridge{}
	if used, max := bridge.ContextUsage(); used != 0 || max != 0 {
		t.Fatal("expected zero usage without agent")
	}
	bridge.agent = &usageAgent{}
	used, max := bridge.ContextUsage()
	if used != 100 || max != 1000 {
		t.Fatalf("got %d/%d, want 100/1000", used, max)
	}
}

type closeAgent struct {
	fakeAgent
	closed chan struct{}
}

func (a *closeAgent) Close() { close(a.closed) }

func TestAgentForClosesReplacedAgent(t *testing.T) {
	closed := make(chan struct{})
	bridge := &aiBridge{}
	bridge.agent = &closeAgent{closed: closed}
	bridge.agentKey = "old\x00model\x00false"
	p := &ai.Provider{Name: "p", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m"}
	if _, err := bridge.agentFor(p, "m", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("replaced agent not closed")
	}
}

func TestSwitchCancelsInFlightRun(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "p1", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m1"})
	store.Upsert(ai.Provider{Name: "p2", Type: ai.ProviderOpenAI, APIKey: "k", DefaultModel: "m2"})
	if err := store.SetActive("p1", "m1"); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store, db: database}
	bridge.agent = &fakeAgent{}
	bridge.agentKey = "p1\x00m1\x00false"

	out, err := bridge.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	bridge.Switch("p2", "m2")
	select {
	case _, ok := <-out:
		if ok {
			for range out {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run still streaming after provider switch")
	}
}

func TestBridgeTasksAndCancel(t *testing.T) {
	bridge := &aiBridge{}
	if bridge.Tasks() != nil {
		t.Fatal("tasks without an agent must be nil")
	}
	bridge.CancelTask("task-1") // no agent: no-op, no panic

	agent := &fakeAgent{snaps: []ai.TaskSnapshot{
		{ID: "task-1", Task: "watch the build", Status: ai.TaskRunning, StartedSecAgo: 3, Tail: []ai.TaskActivity{
			{Kind: "status", Text: "running"},
			{Kind: "tool", Text: "list_tabs {}"},
		}},
	}}
	bridge.agent = agent

	list := bridge.Tasks()
	if len(list) != 1 {
		t.Fatalf("got %d tasks, want 1", len(list))
	}
	e := list[0]
	if e.ID != "task-1" || e.Task != "watch the build" || e.Status != "running" || e.StartedSecAgo != 3 {
		t.Fatalf("entry = %+v", e)
	}
	if len(e.Tail) != 2 || e.Tail[1].Kind != "tool" || e.Tail[1].Text != "list_tabs {}" {
		t.Fatalf("tail = %+v", e.Tail)
	}

	bridge.CancelTask("task-1")
	if len(agent.cancelledTasks) != 1 || agent.cancelledTasks[0] != "task-1" {
		t.Fatalf("cancel not routed to the agent: %v", agent.cancelledTasks)
	}
}
