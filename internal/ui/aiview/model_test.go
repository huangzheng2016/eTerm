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
	m := New(fake, fake, fake)
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

func TestViewFillsFrameExactly(t *testing.T) {
	m := newTestModel(nil)
	out := plain(m.View().Content)
	if n := strings.Count(out, "\n") + 1; n != 32 {
		t.Fatalf("view height = %d lines, want 32", n)
	}
	if !strings.Contains(out, "Ask the AI to read tabs") {
		t.Fatal("missing input placeholder hint")
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

type ctxRunner struct {
	*FakeRunner
	used, max int
}

func (r ctxRunner) ContextUsage() (int, int) { return r.used, r.max }

func TestContextUsageInTitle(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(ctxRunner{fake, 512, 1024}, fake, fake)
	m.SetSize(100, 32)
	if out := plain(m.View().Content); !strings.Contains(out, "context: 50% (512/1k)") {
		t.Fatalf("title missing context usage:\n%s", out)
	}

	m = New(ctxRunner{fake, 114688, 1048576}, fake, fake)
	m.SetSize(100, 32)
	if out := plain(m.View().Content); !strings.Contains(out, "context: 10% (114k/1M)") {
		t.Fatalf("title missing humanized usage:\n%s", out)
	}

	m = New(ctxRunner{fake, 0, 0}, fake, fake)
	m.SetSize(100, 32)
	if strings.Contains(plain(m.View().Content), "context:") {
		t.Fatal("ctx shown without usage data")
	}

	// ctx plus a long model name still fits the frame at 80x24.
	long := strings.Repeat("very-long-provider-name-", 4)
	fake.Add(Provider{Name: long, Type: "openai"})
	fake.Switch(long, "x")
	m = New(ctxRunner{fake, 512, 1024}, fake, fake)
	m.SetSize(80, 24)
	fillConversation(m)
	if n := strings.Count(m.View().Content, "\n") + 1; n != 24 {
		t.Fatalf("ctx + long model name: view height = %d, want 24", n)
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

func TestSendQueuedWhileRunning(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("first")
	m.send()
	m.input.SetValue("second")
	if cmd := m.send(); cmd != nil {
		t.Fatal("send while running must queue, not start a run")
	}
	if len(m.blocks) != 2 || !m.blocks[1].queued {
		t.Fatalf("second send must queue a dim user block: %+v", m.blocks)
	}
	pumpEvents(t, m)
	users := 0
	for _, b := range m.blocks {
		if b.kind == blockUser {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("got %d user blocks, want 2", users)
	}
}

func TestProviderPickerSwitchAndAdd(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
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

func TestEscKeepsRunInBackground(t *testing.T) {
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
	if m.status != statusRunning {
		t.Fatal("esc should keep the run going in the background")
	}
	if m.cancel == nil {
		t.Fatal("esc should not cancel the run")
	}
}

func TestCtrlCInterruptsRun(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "partial"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	m.handleEvent(<-m.events)
	m.chatKey(keyMsg('c', tea.ModCtrl))
	if m.status != statusIdle {
		t.Fatal("status not idle after ctrl+c")
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockSystem || !strings.Contains(last.text, "Interrupted") {
		t.Fatalf("missing interruption marker, got %+v", last)
	}
	if m.events != nil {
		t.Fatal("events should be dropped after ctrl+c")
	}
	m.chatKey(keyMsg('c', tea.ModCtrl))
	if m.input.Value() != "" {
		t.Fatal("idle ctrl+c should clear the input")
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

func TestToolOutputSummaryAndExpand(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventToolCallStart, Text: "read_tab"},
		{Kind: EventToolCallEnd, Text: "line1\nline2\nline3\nline4\nline5"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	m.handleEvent(<-m.events)
	m.handleEvent(<-m.events)

	summary := plain(m.blocks[1].cache)
	if !strings.Contains(summary, "line3") || strings.Contains(summary, "line4") {
		t.Fatalf("summary should show first 3 lines only, got:\n%s", summary)
	}
	if !strings.Contains(summary, "2 more lines") {
		t.Fatal("summary missing more-lines note")
	}

	m.Update(keyMsg('o', tea.ModCtrl))
	expanded := plain(m.blocks[1].cache)
	if !strings.Contains(expanded, "line5") {
		t.Fatal("ctrl+o did not expand tool output")
	}

	m.Update(keyMsg('o', tea.ModCtrl))
	if strings.Contains(plain(m.blocks[1].cache), "line4") {
		t.Fatal("ctrl+o did not collapse tool output")
	}
}

func TestTitleFitsWithVoiceActive(t *testing.T) {
	m := newTestModel(nil)
	m.store.Add(Provider{Name: "a-very-long-provider-name-for-width-testing", Type: "openai", Model: "m"})
	m.store.Switch("a-very-long-provider-name-for-width-testing", "m")
	m.SetVoiceActive(true)
	m.status = statusRunning // spinner visible too

	content, _ := m.chatView()
	line := strings.Split(plain(content), "\n")[0]
	cw := m.contentWidth()
	if w := ansi.StringWidth(line); w > cw {
		t.Fatalf("title width %d exceeds content width %d: %q", w, cw, line)
	}
	if !strings.Contains(line, "REC") {
		t.Fatal("REC missing from title")
	}
}

func TestInjectUserMessageIdleStartsRunKeepsDraft(t *testing.T) {
	m, _ := newSteerTestModel()
	m.input.SetValue("draft")
	if cmd := m.InjectUserMessage("[cron] wake"); cmd == nil {
		t.Fatal("idle inject must start a run")
	}
	if m.status != statusRunning {
		t.Fatal("inject did not start a run")
	}
	if m.input.Value() != "draft" {
		t.Fatalf("draft clobbered: %q", m.input.Value())
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockUser || last.queued || last.text != "[cron] wake" {
		t.Fatalf("block: %+v", last)
	}
}

func TestInjectUserMessageRunningQueuesKeepsDraft(t *testing.T) {
	m, fake := newSteerTestModel()
	m.input.SetValue("first")
	m.send()
	m.input.SetValue("draft")
	if cmd := m.InjectUserMessage("wake"); cmd != nil {
		t.Fatal("inject into a run must not start a second run")
	}
	if m.input.Value() != "draft" {
		t.Fatalf("draft clobbered: %q", m.input.Value())
	}
	fake.mu.Lock()
	queued := append([]string(nil), fake.Queued...)
	fake.mu.Unlock()
	if len(queued) != 1 || queued[0] != "wake" {
		t.Fatalf("runner queue: %v", queued)
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockUser || !last.queued {
		t.Fatalf("injected block must be the dim queued marker: %+v", last)
	}
	if out := plain(last.cache); !strings.Contains(out, "Queued: wake") {
		t.Fatalf("queued marker render: %q", out)
	}
}

func TestInjectUserMessageBuffersInPickerMode(t *testing.T) {
	m, _ := newSteerTestModel()
	m.mode = modeProviders
	if cmd := m.InjectUserMessage("wake one"); cmd != nil {
		t.Fatal("picker-mode inject must buffer, not deliver")
	}
	m.InjectUserMessage("wake two")
	if len(m.blocks) != 0 {
		t.Fatalf("buffered inject added a block: %+v", m.blocks)
	}

	m.mode = modeChat
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32}); cmd == nil {
		t.Fatal("returning to chat must flush buffered messages")
	}
	if len(m.injected) != 0 {
		t.Fatalf("buffer not drained: %v", m.injected)
	}
	var texts []string
	for _, b := range m.blocks {
		if b.kind == blockUser {
			texts = append(texts, b.text)
		}
	}
	if len(texts) != 2 || texts[0] != "wake one" || texts[1] != "wake two" {
		t.Fatalf("flushed blocks: %v", texts)
	}
	if m.status != statusRunning {
		t.Fatal("first flushed message must start a run")
	}
}

func TestInjectUserMessageBufferCapDropsOldest(t *testing.T) {
	m, _ := newSteerTestModel()
	m.mode = modeTasks
	for i := 0; i < maxInjectedBuffer+2; i++ {
		m.InjectUserMessage(string(rune('a' + i)))
	}
	if len(m.injected) != maxInjectedBuffer {
		t.Fatalf("buffer: %d, want %d", len(m.injected), maxInjectedBuffer)
	}
	if m.injected[0] != "c" {
		t.Fatalf("oldest not dropped first: %v", m.injected)
	}
}

func TestClearSessionDropsInjectedBuffer(t *testing.T) {
	m, _ := newSteerTestModel()
	m.mode = modeTasks
	m.InjectUserMessage("wake")
	m.mode = modeChat
	m.clearSession()
	if len(m.injected) != 0 {
		t.Fatalf("buffer survived clearSession: %v", m.injected)
	}
}
