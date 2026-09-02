package aiview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCopyLastReply(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "first reply"},
		{Kind: EventDone},
	})
	m.copyLastReply()
	if !strings.Contains(m.errMsg, "no assistant reply") {
		t.Fatalf("missing error, got %q", m.errMsg)
	}

	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)
	if cmd := m.copyLastReply(); cmd == nil {
		t.Fatal("/copy returned no cmd")
	}
	if !strings.Contains(m.toast, "Copied") {
		t.Fatalf("missing copy toast, got %q", m.toast)
	}
}

func TestInputHistoryRecall(t *testing.T) {
	m := newTestModel([]AgentEvent{{Kind: EventDone}})
	m.input.SetValue("first")
	m.send()
	pumpEvents(t, m)
	m.input.SetValue("second")
	m.send()
	pumpEvents(t, m)

	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "second" {
		t.Fatalf("up = %q, want %q", got, "second")
	}
	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "first" {
		t.Fatalf("up = %q, want %q", got, "first")
	}
	m.chatKey(keyMsg(tea.KeyUp, 0)) // oldest entry sticks
	if got := m.input.Value(); got != "first" {
		t.Fatalf("up at oldest = %q, want %q", got, "first")
	}
	m.chatKey(keyMsg(tea.KeyDown, 0))
	if got := m.input.Value(); got != "second" {
		t.Fatalf("down = %q, want %q", got, "second")
	}
	m.chatKey(keyMsg(tea.KeyDown, 0))
	if got := m.input.Value(); got != "" {
		t.Fatalf("down past newest = %q, want empty draft", got)
	}
	if m.histIdx != -1 {
		t.Fatal("browsing did not end past the newest entry")
	}

	// A non-empty, non-browsing input keeps up for textarea line navigation.
	m.input.SetValue("draft")
	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "draft" {
		t.Fatalf("up with non-empty input = %q, want untouched draft", got)
	}

	// Edits to a recalled entry stick to it; the pre-browse draft returns
	// past the newest entry.
	m.input.SetValue("")
	m.chatKey(keyMsg(tea.KeyUp, 0))
	m.input.SetValue("second edited")
	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "first" {
		t.Fatalf("up after edit = %q, want %q", got, "first")
	}
	m.chatKey(keyMsg(tea.KeyDown, 0))
	if got := m.input.Value(); got != "second edited" {
		t.Fatalf("down = %q, want edited entry", got)
	}
	m.chatKey(keyMsg(tea.KeyDown, 0))
	if got := m.input.Value(); got != "" {
		t.Fatalf("down past newest = %q, want empty draft", got)
	}
}

func TestInputHistorySuppressesSlashMenu(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/help")
	if len(m.history) != 1 || m.history[0] != "/help" {
		t.Fatalf("slash command not recorded: %v", m.history)
	}
	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "/help" {
		t.Fatalf("up = %q, want /help", got)
	}
	if matches := m.slashMatches(); matches != nil {
		t.Fatal("slash menu must stay hidden while browsing history")
	}
}

func TestQueueRecall(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.Events = []AgentEvent{{Kind: EventTextDelta, Text: "slow"}, {Kind: EventDone}}
	m := New(fake, fake, fake)
	m.SetSize(100, 32)

	m.input.SetValue("start")
	m.send()
	m.input.SetValue("queued one")
	m.send()
	if len(fake.Queued) != 1 {
		t.Fatalf("queued = %v, want one message", fake.Queued)
	}
	m.input.SetValue("queued two")
	m.send()

	m.chatKey(keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "queued two" {
		t.Fatalf("recall = %q, want newest queued message", got)
	}
	if len(fake.Queued) != 1 || fake.Queued[0] != "queued one" {
		t.Fatalf("queue after recall = %v", fake.Queued)
	}
	queued := 0
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("queued blocks = %d, want 1", queued)
	}
	pumpEvents(t, m)
}

func TestEditorDoneFillsInput(t *testing.T) {
	m := newTestModel(nil)
	m.Update(editorDoneMsg{text: "from editor"})
	if got := m.input.Value(); got != "from editor" {
		t.Fatalf("input = %q, want editor content", got)
	}
	m.Update(editorDoneMsg{err: errors.New("boom")})
	if !strings.Contains(m.errMsg, "editor") {
		t.Fatalf("missing editor error, got %q", m.errMsg)
	}
}

func TestCtrlGReturnsExecCmd(t *testing.T) {
	m := newTestModel(nil)
	m.input.SetValue("draft")
	_, cmd := m.chatKey(keyMsg('g', tea.ModCtrl))
	if cmd == nil {
		t.Fatal("ctrl+g returned no cmd")
	}
}

func TestSessionsFilter(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.History = []byte("turn")
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	fake.SaveSession("s1", "Fix login bug", "")
	fake.SaveSession("s2", "Write tests", "")

	sendSlash(t, m, "/resume")
	if m.mode != modeSessions {
		t.Fatal("/resume did not open the session picker")
	}
	for _, r := range []rune("FIX") {
		m.Update(keyMsg(r, 0))
	}
	if m.sFilter != "FIX" {
		t.Fatalf("filter = %q", m.sFilter)
	}
	list := m.filteredSessions()
	if len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("case-insensitive filter = %v, want only s1", list)
	}
	if out := plain(m.sessionsView()); !strings.Contains(out, "Fix login bug") || strings.Contains(out, "Write tests") {
		t.Fatalf("filtered view wrong:\n%s", out)
	}

	// Enter restores the filtered entry under the cursor.
	m.Update(keyMsg(tea.KeyEnter, 0))
	if m.mode != modeChat {
		t.Fatal("enter did not leave the picker")
	}
	if m.sessionID != "s1" {
		t.Fatalf("restored session = %q, want s1", m.sessionID)
	}

	// Esc clears the filter first, then closes.
	sendSlash(t, m, "/resume")
	for _, r := range []rune("zzx") {
		m.Update(keyMsg(r, 0))
	}
	m.Update(keyMsg(tea.KeyBackspace, 0))
	if m.sFilter != "zz" {
		t.Fatalf("backspace filter = %q, want zz", m.sFilter)
	}
	for _, r := range []rune("z") {
		m.Update(keyMsg(r, 0))
	}
	if !strings.Contains(plain(m.sessionsView()), "no matching sessions") {
		t.Fatal("empty filter result not rendered")
	}
	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.sFilter != "" || m.mode != modeSessions {
		t.Fatal("esc must clear the filter before closing")
	}
	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.mode != modeChat {
		t.Fatal("esc with empty filter must close the picker")
	}
}

func TestExportSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "export me"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	pumpEvents(t, m)

	if cmd := m.exportSession(); cmd == nil {
		t.Fatal("/export returned no cmd")
	}
	if !strings.Contains(m.toast, "Exported to ~/Downloads/eterm-ai-") {
		t.Fatalf("toast = %q", m.toast)
	}
	matches, err := filepath.Glob(filepath.Join(home, "Downloads", "eterm-ai-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("exported files = %v, err = %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## You\n\nhi", "## Assistant\n\nexport me"} {
		if !strings.Contains(string(data), s) {
			t.Fatalf("export missing %q:\n%s", s, data)
		}
	}
}

func TestExportFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := newTestModel(nil)
	if cmd := m.exportSession(); cmd == nil {
		t.Fatal("/export returned no cmd")
	}
	matches, err := filepath.Glob(filepath.Join(home, "eterm-ai-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("fallback exported files = %v, err = %v", matches, err)
	}
}

func TestExportMarkdown(t *testing.T) {
	m := newTestModel(nil)
	m.blocks = []block{
		{kind: blockUser, text: "question"},
		{kind: blockAssistant, text: "answer", final: true},
		{kind: blockThinking, text: "hmm"},
		{kind: blockTool, text: "terminal", args: "ls -la", output: "file.go\n"},
		{kind: blockSystem, text: "note"},
	}
	md := m.exportMarkdown()
	for _, s := range []string{
		"## You\n\nquestion",
		"## Assistant\n\nanswer",
		"### Thinking\n\nhmm",
		"### Tool: terminal\n\nls -la",
		"```\nfile.go\n```",
		"### System\n\nnote",
	} {
		if !strings.Contains(md, s) {
			t.Fatalf("markdown missing %q:\n%s", s, md)
		}
	}
}

func TestCompact(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.CompactResult = CompactStats{MessagesBefore: 10, MessagesAfter: 5, TokensBefore: 100, TokensAfter: 40}
	m := New(fake, fake, fake)
	m.SetSize(100, 32)

	cmd := m.compactSession()
	if cmd == nil {
		t.Fatal("/compact returned no cmd")
	}
	m.Update(cmd())
	if fake.CompactCalls != 1 {
		t.Fatalf("compact calls = %d", fake.CompactCalls)
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockSystem {
		t.Fatalf("compact result kind = %v, want blockSystem", last.kind)
	}
	want := "Compaction complete (100 → 40 tokens, 10 → 5 messages)"
	if last.text != want {
		t.Fatalf("compact block = %q, want %q", last.text, want)
	}
}

func TestCompactError(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.CompactErr = errors.New("model down")
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	m.Update(m.compactSession()())
	if !strings.Contains(m.errMsg, "model down") {
		t.Fatalf("missing compact error, got %q", m.errMsg)
	}
}

func TestCompactBlockedWhileRunning(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	sendSlash(t, m, "/compact")
	if !strings.Contains(m.errMsg, "run in progress") {
		t.Fatalf("/compact must be refused mid-run, got %q", m.errMsg)
	}
	// /copy and /export stay available: they only read state.
	sendSlash(t, m, "/copy")
	if strings.Contains(m.errMsg, "run in progress") {
		t.Fatal("/copy must stay available mid-run")
	}
	pumpEvents(t, m)
}
