package sshview

import (
	"io"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type gateReader struct {
	release chan struct{}
	once    sync.Once
}

func newGateReader() *gateReader {
	return &gateReader{release: make(chan struct{})}
}

func (g *gateReader) Read(p []byte) (int, error) {
	<-g.release
	return 0, io.EOF
}

func (g *gateReader) Close() error {
	g.once.Do(func() { close(g.release) })
	return nil
}

func watchdogTestModel(t *testing.T) (*Model, *trackingWriteCloser) {
	t.Helper()
	stdin := &trackingWriteCloser{}
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.wdStallAfter = 10 * time.Second
	m.wdMaxHits = 3
	return m, stdin
}

func (m *Model) setWatchdogState(inputAt, outputAt, baseAt time.Time, hits int) {
	m.wdMu.Lock()
	defer m.wdMu.Unlock()
	m.wdInputAt = inputAt
	m.wdOutputAt = outputAt
	m.wdBaseAt = baseAt
	m.wdHits = hits
}

func (m *Model) watchdogState() (inputAt, outputAt, baseAt time.Time, hits int) {
	m.wdMu.Lock()
	defer m.wdMu.Unlock()
	return m.wdInputAt, m.wdOutputAt, m.wdBaseAt, m.wdHits
}

func TestWatchdogQueueInputArmsAndPassiveDoesNot(t *testing.T) {
	m, _ := watchdogTestModel(t)
	if !m.queueInput([]byte("x")) {
		t.Fatal("queueInput failed")
	}
	inputAt, _, baseAt, _ := m.watchdogState()
	if inputAt.IsZero() || baseAt.IsZero() {
		t.Fatal("queueInput did not arm watchdog")
	}

	m2, stdin2 := watchdogTestModel(t)
	if !m2.queueInputPassive([]byte("\x1b[?1;2c")) {
		t.Fatal("queueInputPassive failed")
	}
	inputAt, _, baseAt, _ = m2.watchdogState()
	if !inputAt.IsZero() || !baseAt.IsZero() {
		t.Fatal("emulator-originated input armed watchdog")
	}
	m2.checkWatchdog(time.Now().Add(time.Hour))
	if _, _, _, hits := m2.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0", hits)
	}
	if stdin2.isClosed() {
		t.Fatal("session closed for emulator-originated input")
	}
}

func TestWatchdogNoInputNeverTriggers(t *testing.T) {
	m, stdin := watchdogTestModel(t)
	base := time.Now()
	for i := 1; i <= 5; i++ {
		m.checkWatchdog(base.Add(time.Duration(i) * 30 * time.Second))
	}
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0", hits)
	}
	if stdin.isClosed() {
		t.Fatal("session closed without input")
	}
}

func TestWatchdogInputWithoutOutputTriggersAfterMaxHits(t *testing.T) {
	m, stdin := watchdogTestModel(t)
	t0 := time.Now()
	m.setWatchdogState(t0, time.Time{}, t0, 0)

	m.checkWatchdog(t0.Add(10 * time.Second))
	if _, _, _, hits := m.watchdogState(); hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	m.checkWatchdog(t0.Add(15 * time.Second))
	if _, _, baseAt, hits := m.watchdogState(); hits != 1 || !baseAt.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("hits = %d baseAt = %v, want 1 hit with baseline reset", hits, baseAt)
	}
	m.checkWatchdog(t0.Add(30 * time.Second))
	if _, _, _, hits := m.watchdogState(); hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
	m.checkWatchdog(t0.Add(50 * time.Second))
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0 after trigger", hits)
	}
	if !stdin.isClosed() {
		t.Fatal("session not closed after max hits")
	}
	m.mu.Lock()
	err := m.endErr
	m.mu.Unlock()
	if err != errOutputStalled {
		t.Fatalf("endErr = %v, want %v", err, errOutputStalled)
	}
}

func TestWatchdogOutputResetsHits(t *testing.T) {
	m, stdin := watchdogTestModel(t)
	t0 := time.Now()
	m.setWatchdogState(t0, time.Time{}, t0, 2)

	m.noteWatchdogOutput(t0.Add(5 * time.Second))
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0 after output", hits)
	}
	m.checkWatchdog(t0.Add(60 * time.Second))
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0 (output after input)", hits)
	}
	if stdin.isClosed() {
		t.Fatal("session closed despite output")
	}
}

func TestWatchdogSlowCommandDoesNotAccumulate(t *testing.T) {
	m, stdin := watchdogTestModel(t)
	t0 := time.Now()
	m.noteWatchdogInput(t0)
	m.noteWatchdogOutput(t0.Add(time.Second))
	for i := 1; i <= 6; i++ {
		m.checkWatchdog(t0.Add(time.Duration(i) * 20 * time.Second))
	}
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0 for silent command with echo", hits)
	}
	if stdin.isClosed() {
		t.Fatal("session closed for slow command")
	}
}

func TestWatchdogChunkMsgRecordsOutput(t *testing.T) {
	m, _ := watchdogTestModel(t)
	t0 := time.Now()
	m.setWatchdogState(t0, time.Time{}, t0, 2)

	m.Update(ChunkMsg{StreamID: m.StreamID() + 1, Data: []byte("stale")})
	if _, _, _, hits := m.watchdogState(); hits != 2 {
		t.Fatalf("hits = %d, want 2 (stale chunk ignored)", hits)
	}
	m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("out")})
	_, outputAt, _, hits := m.watchdogState()
	if hits != 0 {
		t.Fatalf("hits = %d, want 0", hits)
	}
	if outputAt.IsZero() {
		t.Fatal("chunk did not record output time")
	}
}

func TestWatchdogTriggerLeadsToAutoReconnect(t *testing.T) {
	stdin := &trackingWriteCloser{}
	stdout := newGateReader()
	done := make(chan error, 1)
	done <- nil
	sess := &internalssh.InteractiveSession{Stdin: stdin, Stdout: stdout, Done: done}
	sess.AddCloser(stdout)
	m := New(sess, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.wdStallAfter = 10 * time.Second
	m.wdMaxHits = 3
	m.SetRemoteReconnect(&types.RemoteReconnect{Tmux: true, SessionID: "work"})

	go m.readLoop()
	go m.watchDone()
	chunkCmd := waitChunk(m)

	t0 := time.Now()
	m.setWatchdogState(t0, time.Time{}, t0, 0)
	for i := 1; i <= 3; i++ {
		m.checkWatchdog(t0.Add(time.Duration(i) * 10 * time.Second))
	}
	if !stdin.isClosed() {
		t.Fatal("session not closed after max hits")
	}

	msg := chunkCmd()
	doneMsg, ok := msg.(StreamDoneMsg)
	if !ok {
		t.Fatalf("got %T, want StreamDoneMsg", msg)
	}
	if doneMsg.Err != errOutputStalled {
		t.Fatalf("StreamDoneMsg.Err = %v, want %v", doneMsg.Err, errOutputStalled)
	}
	_, cmd := m.Update(doneMsg)
	if cmd == nil {
		t.Fatal("no reconnect command")
	}
	out := cmd()
	reconnect, ok := out.(types.RemoteShellReconnectMsg)
	if !ok {
		t.Fatalf("got %T, want RemoteShellReconnectMsg", out)
	}
	if !reconnect.Auto || reconnect.Attempt != 1 || reconnect.MaxAttempts != remoteTmuxReconnectAttempts {
		t.Fatalf("reconnect = %+v, want auto attempt 1/%d", reconnect, remoteTmuxReconnectAttempts)
	}
	if !m.Disconnected() {
		t.Fatal("model not marked disconnected")
	}
	if label := m.ReconnectingLabel(); label != "RECONNECTING (1/3)" {
		t.Fatalf("label = %q", label)
	}
}

func TestWatchdogTickStartsOnFirstKeypress(t *testing.T) {
	m, _ := watchdogTestModel(t)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if cmd == nil {
		t.Fatal("watchdog tick not started on first keypress")
	}
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 'b', Text: "b"}))
	if cmd != nil {
		t.Fatal("watchdog tick started twice")
	}
}

func TestWatchdogTickMsgReissuesTick(t *testing.T) {
	m, _ := watchdogTestModel(t)
	_, cmd := m.Update(watchdogTickMsg{StreamID: m.StreamID()})
	if cmd == nil {
		t.Fatal("tick not reissued")
	}
	_, cmd = m.Update(watchdogTickMsg{StreamID: m.StreamID() + 1})
	if cmd != nil {
		t.Fatal("stale stream tick not ignored")
	}
}

func TestWatchdogResumeSessionResetsState(t *testing.T) {
	m, _ := watchdogTestModel(t)
	t0 := time.Now()
	m.setWatchdogState(t0, time.Time{}, t0, 2)

	pr, _ := io.Pipe()
	sess := &internalssh.InteractiveSession{Stdout: pr, Done: make(chan error, 1)}
	m.ResumeSession(sess)
	inputAt, outputAt, baseAt, hits := m.watchdogState()
	if hits != 0 || !inputAt.IsZero() || !outputAt.IsZero() || !baseAt.IsZero() {
		t.Fatalf("watchdog state not cleared on resume: hits=%d", hits)
	}
	m.checkWatchdog(t0.Add(time.Hour))
	if _, _, _, hits := m.watchdogState(); hits != 0 {
		t.Fatalf("hits = %d, want 0 after resume", hits)
	}
}
