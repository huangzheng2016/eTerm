package sessionlistview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

func replayTestData(t *testing.T) []byte {
	t.Helper()
	r := sshview.NewRecorder(time.Now())
	r.Resize(5, 20)
	r.Output([]byte("first"))
	time.Sleep(2 * time.Millisecond)
	r.Output([]byte("\rsecond"))
	data, _, _ := r.Close()
	return data
}

func TestReplaySeekRebuildsTerminal(t *testing.T) {
	data := replayTestData(t)
	events, err := sshview.DecodeReplay(data)
	if err != nil {
		t.Fatal(err)
	}
	r, err := newReplayState(data, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.seek(time.Duration(events[len(events)-1].At) * time.Millisecond)
	if got := r.emu.Render(); got == "" {
		t.Fatal("empty replay screen")
	}
	r.seek(0)
	if r.next > 2 {
		t.Fatalf("seek did not reset event cursor: %d", r.next)
	}
}

func TestReplayAppliesInitialEventsAndRestartsAtEnd(t *testing.T) {
	r, err := newReplayState(replayTestData(t), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if r.next == 0 {
		t.Fatal("initial events were not applied")
	}
	r.seek(r.duration)
	cmd := r.toggle()
	if cmd == nil || !r.playing || r.pos != 0 {
		t.Fatalf("playing=%v pos=%v cmd=%v", r.playing, r.pos, cmd)
	}
}

func TestReplaySpaceKeyTogglesPlayback(t *testing.T) {
	r, err := newReplayState(replayTestData(t), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	m := New(nil)
	m.detail = true
	m.replay = r
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}))
	if !r.playing || cmd == nil {
		t.Fatalf("playing=%v cmd=%v key=%q", r.playing, cmd, tea.KeyPressMsg(tea.Key{Code: ' ', Text: " "}).String())
	}
	if hint := m.StatusBarHint(); hint == "" {
		t.Fatal("missing replay status hint")
	}
}

func TestReplayProgressStaysAtBottomWithWideGrowingScreen(t *testing.T) {
	recorder := sshview.NewRecorder(time.Now())
	recorder.Resize(20, 100)
	recorder.Output([]byte(strings.Repeat("x", 200) + "\n" + strings.Repeat("y", 200)))
	data, duration, _ := recorder.Close()
	r, err := newReplayState(data, duration)
	if err != nil {
		t.Fatal(err)
	}
	r.seek(r.duration)
	m := New(nil)
	m.SetSize(30, 12)
	m.loaded = true
	m.detail = true
	m.replay = r
	m.rows = []db.ConnectionHistory{{Label: "replay"}}
	view := m.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) != 12 || !strings.Contains(lines[len(lines)-1], "[paused]") {
		t.Fatalf("height=%d last=%q view=%q", len(lines), lines[len(lines)-1], view)
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 30 {
			t.Fatalf("line width=%d exceeds viewport: %q", width, line)
		}
	}
}

func TestReplayAltScreenKeepsBottomRowsWhenViewportIsShorter(t *testing.T) {
	recorder := sshview.NewRecorder(time.Now())
	recorder.Resize(10, 20)
	recorder.Output([]byte("\x1b[?1049h\x1b[Hrow00\r\nrow01\r\nrow02\r\nrow03\r\nrow04\r\nrow05\r\nrow06\r\nrow07\r\nrow08\r\nrow09"))
	data, duration, _ := recorder.Close()
	r, err := newReplayState(data, duration)
	if err != nil {
		t.Fatal(err)
	}
	r.seek(r.duration)
	m := New(nil)
	m.SetSize(20, 8)
	m.loaded = true
	m.detail = true
	m.replay = r
	m.rows = []db.ConnectionHistory{{Label: "replay"}}
	view := m.View().Content
	if !strings.Contains(view, "row09") || strings.Contains(view, "row00") {
		t.Fatalf("alt-screen viewport did not keep bottom rows: %q", view)
	}
}

func TestReplayNormalScreenKeepsBottomRowsWhenViewportIsShorter(t *testing.T) {
	recorder := sshview.NewRecorder(time.Now())
	recorder.Resize(10, 20)
	recorder.Output([]byte("\x1b[Hrow00\r\nrow01\r\nrow02\r\nrow03\r\nrow04\r\nrow05\r\nrow06\r\nrow07\r\nrow08\r\nrow09"))
	data, duration, _ := recorder.Close()
	r, err := newReplayState(data, duration)
	if err != nil {
		t.Fatal(err)
	}
	r.seek(r.duration)
	m := New(nil)
	m.SetSize(20, 8)
	m.loaded = true
	m.detail = true
	m.replay = r
	m.rows = []db.ConnectionHistory{{Label: "replay"}}
	view := m.View().Content
	if !strings.Contains(view, "row09") || strings.Contains(view, "row00") {
		t.Fatalf("normal viewport did not keep bottom rows: %q", view)
	}
}

func TestReplayHidesMetadataWhilePlayingAndRestoresItWhenPaused(t *testing.T) {
	r, err := newReplayState(replayTestData(t), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	m := New(nil)
	m.SetSize(40, 10)
	m.loaded = true
	m.detail = true
	m.replay = r
	m.rows = []db.ConnectionHistory{{Label: "unique-host-label", Source: "ssh", ConnectedAt: time.Now()}}
	paused := m.View().Content
	if !strings.Contains(paused, "unique-host-label") {
		t.Fatalf("paused view missing metadata: %q", paused)
	}
	r.playing = true
	playing := m.View().Content
	if strings.Contains(playing, "unique-host-label") {
		t.Fatalf("playing view contains metadata: %q", playing)
	}
	playingLines := strings.Split(playing, "\n")
	if len(playingLines) != 10 || !strings.Contains(playingLines[0], "[playing]") || strings.Contains(playingLines[9], "[playing]") {
		t.Fatalf("playing viewport is not fixed: %q", playing)
	}
	r.playing = false
	if restored := m.View().Content; !strings.Contains(restored, "unique-host-label") {
		t.Fatalf("paused metadata was not restored: %q", restored)
	}
}

func TestStaleReplayTickDoesNotAdvanceReplacement(t *testing.T) {
	old, err := newReplayState(replayTestData(t), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	current, err := newReplayState(replayTestData(t), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	current.playing = true
	current.lastTick = time.Now().Add(-time.Second)
	m := New(nil)
	m.replay = current
	base := current.pos
	m.Update(replayTickMsg{replay: old, at: time.Now()})
	if current.pos != base {
		t.Fatalf("replacement advanced to %v", current.pos)
	}
}

func TestParseReplayJump(t *testing.T) {
	for input, want := range map[string]time.Duration{
		"01:02:03": 3723 * time.Second,
		"+30s":     90 * time.Second,
		"-1m":      0,
	} {
		got, err := parseReplayJump(input, time.Minute)
		if err != nil || got != want {
			t.Fatalf("parse %q = %v, %v want %v", input, got, err, want)
		}
	}
}
