package aiview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sendSlash(t *testing.T, m *Model, input string) {
	t.Helper()
	m.input.SetValue(input)
	m.send()
}

func TestSlashHelpListsCommands(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/help")
	if m.status == statusRunning {
		t.Fatal("/help must not start a run")
	}
	if m.input.Value() != "" {
		t.Fatal("input not cleared after /help")
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockSystem {
		t.Fatalf("help block kind = %v, want blockSystem", last.kind)
	}
	for _, s := range []string{"/model", "/new", "/resume", "/fork", "/undo", "/tasks", "ctrl+c", "ctrl+o", "ctrl+p", "esc close"} {
		if !strings.Contains(last.text, s) {
			t.Fatalf("help text missing %q", s)
		}
	}
}

func TestSlashUnknownKeepsInput(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/frobnicate")
	if m.input.Value() != "/frobnicate" {
		t.Fatal("unknown command must keep the input")
	}
	if m.status != statusError || !strings.Contains(m.errMsg, "unknown command") {
		t.Fatalf("missing inline error: status=%v err=%q", m.status, m.errMsg)
	}
	if !strings.Contains(plain(m.View().Content), "unknown command") {
		t.Fatal("error not rendered")
	}
}

func TestSlashModelOpensPicker(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/model")
	if m.mode != modeProviders {
		t.Fatalf("mode = %v, want modeProviders", m.mode)
	}
}

func TestSlashNewStartsFreshSession(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)
	m.saveNow()
	if len(fake.sessions) != 1 {
		t.Fatal("session not persisted")
	}
	oldID := m.sessionID

	sendSlash(t, m, "/new")
	if fake.resets != 1 {
		t.Fatal("agent history not reset")
	}
	if m.sessionID != "" {
		t.Fatal("session id not reset")
	}
	if len(m.blocks) != 1 || m.blocks[0].kind != blockSystem {
		t.Fatalf("expected only the new-session marker, got %+v", m.blocks)
	}
	if fake.sessions[0].entry.ID != oldID {
		t.Fatal("old session row lost")
	}
}

func TestSlashUndoRewindsOneTurn(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.input.SetValue("first")
	m.send()
	pumpEvents(t, m)
	m.input.SetValue("second")
	m.send()
	pumpEvents(t, m)

	sendSlash(t, m, "/undo")
	if fake.undoCalls != 1 {
		t.Fatal("agent history not truncated")
	}
	for _, b := range m.blocks {
		if b.kind == blockUser && b.text == "second" {
			t.Fatal("undone user block still present")
		}
	}
	users := 0
	for _, b := range m.blocks {
		if b.kind == blockUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("got %d user blocks, want 1", users)
	}
	// The undo is persisted: the stored history is updated too.
	if len(fake.sessions) != 1 || fake.sessions[0].entry.Title != "first" {
		t.Fatalf("session not saved after undo: %+v", fake.sessions)
	}

	sendSlash(t, m, "/undo")
	if len(m.blocks) != 0 {
		t.Fatalf("second undo must empty the panel, got %+v", m.blocks)
	}
	sendSlash(t, m, "/undo")
	if !strings.Contains(m.errMsg, "nothing to undo") {
		t.Fatalf("missing nothing-to-undo error, got %q", m.errMsg)
	}
}

func TestSlashResumeRestoresSession(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.History = []byte(`[{"role":"user","content":"first question"},{"role":"assistant","content":"first answer"},{"role":"tool","content":"tool output"},{"role":"assistant","content":""},{"role":"user","content":"second question"},{"role":"assistant","content":"second answer"}]`)
	fake.SaveSession("s1", "first question", "")
	m := New(fake, fake, fake)
	m.SetSize(100, 32)

	sendSlash(t, m, "/resume")
	if m.mode != modeSessions {
		t.Fatalf("mode = %v, want modeSessions", m.mode)
	}
	if out := plain(m.View().Content); !strings.Contains(out, "first question") {
		t.Fatalf("picker missing session title:\n%s", out)
	}
	m.Update(keyMsg(tea.KeyEnter, 0))
	if m.mode != modeChat {
		t.Fatal("picker did not close on enter")
	}
	if m.sessionID != "s1" {
		t.Fatalf("session id = %q, want s1", m.sessionID)
	}
	var kinds []blockKind
	for _, b := range m.blocks {
		kinds = append(kinds, b.kind)
	}
	want := []blockKind{blockSystem, blockUser, blockAssistant, blockUser, blockAssistant}
	if len(kinds) != len(want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("got %v, want %v", kinds, want)
		}
	}
	out := plain(m.View().Content)
	for _, s := range []string{"restored session", "second answer"} {
		if !strings.Contains(out, s) {
			t.Fatalf("view missing %q", s)
		}
	}
	if strings.Contains(out, "tool output") {
		t.Fatal("tool turns must not rebuild into blocks")
	}
}

func TestSlashResumeEmptyShowsError(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/resume")
	if m.mode == modeSessions {
		t.Fatal("picker opened without saved sessions")
	}
	if !strings.Contains(m.errMsg, "no saved sessions") {
		t.Fatalf("missing error, got %q", m.errMsg)
	}
}

func TestSlashForkCopiesSession(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)
	m.saveNow()
	oldID := m.sessionID

	sendSlash(t, m, "/fork")
	if m.sessionID == "" || m.sessionID == oldID {
		t.Fatal("fork did not allocate a new session id")
	}
	if len(fake.sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(fake.sessions))
	}
	fork := fake.sessions[0]
	if fork.entry.ID != m.sessionID || fork.forkOf != oldID {
		t.Fatalf("fork row = %+v (forkOf %q), want id %q forkOf %q", fork.entry, fork.forkOf, m.sessionID, oldID)
	}
}

func TestSlashForkEmptyShowsError(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/fork")
	if !strings.Contains(m.errMsg, "nothing to fork") {
		t.Fatalf("missing error, got %q", m.errMsg)
	}
}

func TestSlashForkFlushesPendingSave(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.input.SetValue("hi")
	m.send()
	for {
		ev := <-m.events
		m.Update(agentEventMsg{ev: ev})
		if ev.Kind == EventDone {
			break
		}
	}
	// Fork inside the debounce window: the pending tick has not fired, so
	// without a flush the parent's last turn would never be saved.
	sendSlash(t, m, "/fork")
	forkID := m.sessionID
	m.Update(saveTickMsg{seq: m.saveSeq})

	if len(fake.sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (parent flushed + fork)", len(fake.sessions))
	}
	var parent *fakeSession
	for i := range fake.sessions {
		if fake.sessions[i].entry.ID != forkID {
			parent = &fake.sessions[i]
		}
	}
	if parent == nil || len(parent.history) == 0 {
		t.Fatal("parent session lost its last turn")
	}
}

func TestSlashRejectionClearedOnFinish(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	m.input.SetValue("/new")
	m.send()
	if !strings.Contains(m.errMsg, "run in progress") {
		t.Fatalf("rejection not shown, got %q", m.errMsg)
	}
	pumpEvents(t, m)
	if m.errMsg != "" {
		t.Fatalf("stale error after run finished: %q", m.errMsg)
	}
	if strings.Contains(plain(m.View().Content), "run in progress") {
		t.Fatal("stale error still rendered")
	}
}

func TestSlashBlockedWhileRunning(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	m.input.SetValue("/new")
	m.send()
	if m.status != statusRunning {
		t.Fatal("run state clobbered by slash command")
	}
	if m.input.Value() != "/new" {
		t.Fatal("blocked command must keep the input")
	}
	if !strings.Contains(m.errMsg, "run in progress") {
		t.Fatalf("missing busy error, got %q", m.errMsg)
	}
	pumpEvents(t, m)
}

func TestAutosaveScheduledOnRunEnd(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.input.SetValue("hi")
	m.send()
	if m.saveSeq != 0 {
		t.Fatal("save scheduled before run end")
	}
	for {
		ev := <-m.events
		m.Update(agentEventMsg{ev: ev})
		if ev.Kind == EventDone {
			break
		}
	}
	if m.saveSeq != 1 {
		t.Fatalf("saveSeq = %d, want 1 after run end", m.saveSeq)
	}
	// Stale ticks are ignored; the matching one saves.
	m.Update(saveTickMsg{seq: 0})
	if len(fake.sessions) != 0 {
		t.Fatal("stale tick triggered a save")
	}
	m.Update(saveTickMsg{seq: 1})
	if len(fake.sessions) != 1 || fake.sessions[0].entry.Title != "hi" {
		t.Fatalf("autosave did not persist the session: %+v", fake.sessions)
	}
}

func TestSlashMenuShowsAllOnSlashInput(t *testing.T) {
	m := newTestModel(nil)
	if out := plain(m.View().Content); strings.Contains(out, "/model") {
		t.Fatal("menu must stay hidden on empty input")
	}

	m.input.SetValue("/")
	out := plain(m.View().Content)
	for _, s := range []string{"/model", "/new", "/resume", "/fork", "/undo", "/tasks", "/help", "pick model", "background agents"} {
		if !strings.Contains(out, s) {
			t.Fatalf("menu missing %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "▸ /model") {
		t.Fatal("cursor not on the first command")
	}
	if n := strings.Count(out, "\n") + 1; n != 32 {
		t.Fatalf("menu must not grow the frame: view height = %d, want 32", n)
	}
}

func TestSlashMenuFiltersByPrefix(t *testing.T) {
	m := newTestModel(nil)
	m.input.SetValue("/re")
	out := plain(m.View().Content)
	if !strings.Contains(out, "/resume") {
		t.Fatal("menu missing the prefix match")
	}
	if strings.Contains(out, "/model") {
		t.Fatal("menu not filtered by prefix")
	}

	m.input.SetValue("hello")
	out = plain(m.View().Content)
	if strings.Contains(out, "/resume") {
		t.Fatal("menu visible without a / prefix")
	}
}

func TestSlashMenuNavigateAndComplete(t *testing.T) {
	m := newTestModel(nil)
	m.input.SetValue("/")
	m.Update(keyMsg(tea.KeyDown, 0))
	if out := plain(m.View().Content); !strings.Contains(out, "▸ /new") {
		t.Fatalf("down did not move the cursor:\n%s", out)
	}
	m.Update(keyMsg(tea.KeyUp, 0))
	if out := plain(m.View().Content); !strings.Contains(out, "▸ /model") {
		t.Fatalf("up did not move the cursor back:\n%s", out)
	}
	m.Update(keyMsg(tea.KeyDown, 0))
	m.Update(keyMsg(tea.KeyDown, 0))
	m.Update(keyMsg(tea.KeyTab, 0))
	if m.input.Value() != "/resume" {
		t.Fatalf("tab completion: input = %q, want /resume", m.input.Value())
	}

	m.input.SetValue("/fo")
	m.Update(keyMsg(tea.KeyRight, 0))
	if m.input.Value() != "/fork" {
		t.Fatalf("right completion: input = %q, want /fork", m.input.Value())
	}
}

func TestSlashMenuEnterCompletesThenRuns(t *testing.T) {
	m := newTestModel(nil)
	m.input.SetValue("/fo")
	m.Update(keyMsg(tea.KeyEnter, 0))
	if m.input.Value() != "/fork" {
		t.Fatalf("enter did not complete: input = %q", m.input.Value())
	}
	if m.errMsg != "" {
		t.Fatalf("completion must not run the command: %q", m.errMsg)
	}
	m.Update(keyMsg(tea.KeyEnter, 0))
	if !strings.Contains(m.errMsg, "nothing to fork") {
		t.Fatalf("second enter must run the command: %q", m.errMsg)
	}
}

func TestSlashMenuEscDismisses(t *testing.T) {
	m := newTestModel(nil)
	m.Init() // focus the textarea so it accepts typed keys
	m.input.SetValue("/mo")
	_, cmd := m.Update(keyMsg(tea.KeyEscape, 0))
	if cmd != nil {
		t.Fatal("esc with a / draft must not close the panel")
	}
	if out := plain(m.View().Content); strings.Contains(out, "/model") {
		t.Fatal("menu still visible after esc")
	}
	m.Update(keyMsg('d', 0)) // any edit re-arms the menu
	if out := plain(m.View().Content); !strings.Contains(out, "/model") {
		t.Fatal("menu did not reappear after an edit")
	}
}
