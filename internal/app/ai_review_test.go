package app

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
)

// fakeAgent mimics ai.Agent: Run streams until ctx ends (then closes, like
// the real runner), and Clear blocks while a run holds the agent mutex.
type fakeAgent struct{ clearRelease chan struct{} }

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

func TestCtrlLReturnsPromptlyMidRun(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	bridge := &aiBridge{}
	bridge.agent = &fakeAgent{clearRelease: release}
	a := App{
		aiView:    aiview.New(bridge, bridge),
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
	bridge.agentKey = "p\x00m"

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
