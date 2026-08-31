package aiview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func plain(s string) string { return ansi.Strip(s) }

func keyMsg(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: code, Mod: mod}
	if mod == 0 && code >= ' ' && code < 0x7f {
		k.Text = string(code)
	}
	return k
}

func newTestModel(events []AgentEvent) *Model {
	fake := NewFakeRunner()
	fake.Events = events
	fake.Delay = 0
	m := New(fake, fake)
	m.SetSize(100, 32)
	return m
}

func pumpEvents(t *testing.T, m *Model) {
	t.Helper()
	for {
		ev, ok := <-m.events
		if !ok {
			return
		}
		if m.handleEvent(ev) {
			return
		}
	}
}

func TestRenderSmokeEmpty(t *testing.T) {
	m := newTestModel(nil)

	out := plain(m.View().Content)
	if !strings.Contains(out, "AI Assistant") {
		t.Fatal("missing title")
	}
	if !strings.Contains(out, "openai") {
		t.Fatal("missing active provider")
	}
	if !strings.Contains(out, "esc close") {
		t.Fatal("missing hint line")
	}
}

func TestSendStreamsAndFinalizes(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventThinkingDelta, Text: "let me check"},
		{Kind: EventTextDelta, Text: "hello "},
		{Kind: EventTextDelta, Text: "**world**\n\n```sh\nls\n```"},
		{Kind: EventToolCallStart, Text: "terminal"},
		{Kind: EventToolCallEnd},
		{Kind: EventTextDelta, Text: "done reply"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi there")
	if cmd := m.send(); cmd == nil {
		t.Fatal("send returned no cmd")
	}
	if m.status != statusRunning {
		t.Fatal("status not running after send")
	}
	pumpEvents(t, m)
	if m.status != statusIdle {
		t.Fatal("status not idle after done")
	}

	var kinds []blockKind
	for _, b := range m.blocks {
		kinds = append(kinds, b.kind)
	}
	want := []blockKind{blockUser, blockThinking, blockAssistant, blockTool, blockAssistant}
	if len(kinds) != len(want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("got %v, want %v", kinds, want)
		}
	}

	out := plain(m.View().Content)
	for _, s := range []string{"hi there", "let me check", "terminal", "done reply"} {
		if !strings.Contains(out, s) {
			t.Fatalf("view missing %q", s)
		}
	}
	for i, b := range m.blocks {
		if b.kind == blockAssistant && !b.final {
			t.Fatalf("assistant block %d not finalized", i)
		}
	}
}

func TestStreamingTransientThenHighlight(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "```go\nfmt.Println()\n```"},
		{Kind: EventDone},
	})
	m.input.SetValue("code")
	m.send()

	ev := <-m.events
	m.handleEvent(ev)
	m.flush()
	streaming := m.blocks[1].cache
	if !strings.Contains(streaming, "fmt.Println") {
		t.Fatal("transient render missing code text")
	}

	ev = <-m.events
	if !m.handleEvent(ev) {
		t.Fatal("expected terminal event")
	}
	final := m.blocks[1].cache
	if !m.blocks[1].final {
		t.Fatal("block not finalized")
	}
	if final == streaming {
		t.Fatal("final render should differ from transient (highlighting)")
	}
}

func TestErrorState(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventError, Text: "provider unreachable"},
	})
	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)
	if m.status != statusError {
		t.Fatal("status not error")
	}
	if !strings.Contains(plain(m.View().Content), "provider unreachable") {
		t.Fatal("view missing error message")
	}
}

func TestEscEmitsClose(t *testing.T) {
	m := newTestModel(nil)
	_, cmd := m.Update(keyMsg(tea.KeyEscape, 0))
	if cmd == nil {
		t.Fatal("esc returned no cmd")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("got %T, want CloseMsg", cmd())
	}
}

func TestCtrlLClearsSession(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "reply"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)
	if len(m.blocks) == 0 {
		t.Fatal("expected blocks")
	}
	m.Update(keyMsg('l', tea.ModCtrl))
	if len(m.blocks) != 0 {
		t.Fatal("session not cleared")
	}
	if m.status != statusIdle {
		t.Fatal("status not idle after clear")
	}
}

func TestScrollKeys(t *testing.T) {
	m := newTestModel(nil)
	for i := 0; i < 40; i++ {
		m.blocks = append(m.blocks, block{kind: blockUser, text: strings.Repeat("line ", 4)})
	}
	m.renderAll()
	m.viewport.GotoTop()
	if m.viewport.AtBottom() {
		t.Fatal("expected not at bottom after GotoTop")
	}
	m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.viewport.YOffset() == 0 {
		t.Fatal("pgdown did not scroll")
	}
	m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.viewport.YOffset() != 0 {
		t.Fatal("pgup did not scroll back")
	}
}

func TestSendIgnoredWhileRunning(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("first")
	m.send()
	m.input.SetValue("second")
	if cmd := m.send(); cmd != nil {
		t.Fatal("send while running should be ignored")
	}
	pumpEvents(t, m)
	users := 0
	for _, b := range m.blocks {
		if b.kind == blockUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("got %d user blocks, want 1", users)
	}
}

func TestProviderPickerSwitchAndAdd(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake)
	m.SetSize(100, 32)

	m.Update(keyMsg('p', tea.ModCtrl))
	if m.mode != modeProviders {
		t.Fatal("ctrl+p did not open providers")
	}
	out := plain(m.View().Content)
	if !strings.Contains(out, "anthropic") {
		t.Fatal("provider list missing anthropic")
	}

	m.Update(keyMsg(tea.KeyDown, 0))
	m.Update(keyMsg(tea.KeyEnter, 0))
	if fake.Active() != "anthropic" {
		t.Fatalf("active = %q, want anthropic", fake.Active())
	}

	m.Update(keyMsg('a', 0))
	if m.mode != modeProviderForm {
		t.Fatal("a did not open add form")
	}
	vals := []string{"kimi", "openai", "https://api.moonshot.cn/v1", "sk-test", "kimi-k2"}
	for i, v := range vals {
		m.form.inputs[i].SetValue(v)
	}
	for i := 0; i < len(vals)-1; i++ {
		m.Update(keyMsg(tea.KeyTab, 0))
	}
	m.Update(keyMsg(tea.KeyEnter, 0))
	if m.mode != modeProviders {
		t.Fatal("submit did not return to list")
	}
	found := false
	for _, e := range fake.Models() {
		if e.Label == "kimi" && e.Model == "kimi-k2" {
			found = true
		}
	}
	if !found {
		t.Fatal("provider not added")
	}

	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.mode != modeChat {
		t.Fatal("esc did not return to chat")
	}
}

func TestThrottleFlushScheduling(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "chunk"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	ev := <-m.events
	m.handleEvent(ev)
	if !m.dirty {
		t.Fatal("expected dirty after delta")
	}
	m.flush()
	if m.dirty {
		t.Fatal("expected clean after flush")
	}
	if !strings.Contains(plain(m.viewport.GetContent()), "chunk") {
		t.Fatal("viewport missing streamed content")
	}
}

func TestEscCancelsRun(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	if m.status != statusRunning {
		t.Fatal("status not running")
	}
	m.chatKey(keyMsg(tea.KeyEscape, 0))
	if m.status != statusIdle {
		t.Fatal("status not idle after esc")
	}
	if m.cancel != nil {
		t.Fatal("run not cancelled after esc")
	}
}

func TestCtrlLDropsInFlightEvent(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "stale"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	ev := <-m.events
	m.Update(keyMsg('l', tea.ModCtrl))
	if len(m.blocks) != 0 {
		t.Fatal("session not cleared")
	}
	m.Update(agentEventMsg{ev: ev})
	if len(m.blocks) != 0 {
		t.Fatal("in-flight event re-appended a ghost block")
	}
}

func TestToolCallEndRerenders(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventToolCallStart, Text: "terminal"},
		{Kind: EventToolCallEnd},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	m.handleEvent(<-m.events)
	if !strings.Contains(plain(m.blocks[1].cache), "running") {
		t.Fatal("tool block should show running after start")
	}
	m.handleEvent(<-m.events)
	if !strings.Contains(plain(m.blocks[1].cache), "done") {
		t.Fatal("tool block not re-rendered on end")
	}
}
