package daemon

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

func dataFrame(t *testing.T, id uint32, seq uint64, data string) []byte {
	t.Helper()
	frame, region := relay.DataFrameBuf(id, seq, len(data))
	copy(region, data)
	return frame
}

func frameData(t *testing.T, b []byte) string {
	t.Helper()
	_, data, err := relay.ParseData(b[relay.HeaderLen:])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDrainDataDropsOnlyTargetStream(t *testing.T) {
	s := newFrameSender()
	_ = s.sendData(dataFrame(t, 1, 0, "a"))
	_ = s.sendData(dataFrame(t, 2, 0, "b"))
	_ = s.sendData(dataFrame(t, 1, 1, "c"))
	_ = s.sendData(dataFrame(t, 2, 1, "d"))

	s.drainData(1)

	var got [][]byte
	for {
		select {
		case b := <-s.data:
			got = append(got, b)
		default:
			if len(got) != 2 {
				t.Fatalf("queued frames = %d, want 2", len(got))
			}
			for _, b := range got {
				if relay.PeekStreamID(b) != 2 {
					t.Fatalf("frame for stream %d survived drain of stream 1", relay.PeekStreamID(b))
				}
			}
			if frameData(t, got[0]) != "b" || frameData(t, got[1]) != "d" {
				t.Fatalf("kept payloads = %q,%q", frameData(t, got[0]), frameData(t, got[1]))
			}
			return
		}
	}
}

func TestAttachClampedKeepsOtherStreamFrames(t *testing.T) {
	fake1 := newDaemonFakeSession()
	fake2 := newDaemonFakeSession()
	sr1 := newStreamRelay(fake1.is)
	sr2 := newStreamRelay(fake2.is)
	t.Cleanup(func() {
		sr1.shutdown()
		sr2.shutdown()
	})
	sender := newFrameSender()
	_ = sender.sendData(dataFrame(t, 1, 0, "drop"))
	_ = sender.sendData(dataFrame(t, 2, 0, "keep"))

	openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: 1}
	if err := sr1.attachClamped(1, 0, sender, openOK); err != nil {
		t.Fatal(err)
	}
	if got := sr1.sidV.Load(); got != 1 {
		t.Fatalf("sidV = %d, want 1", got)
	}

	b := <-sender.data
	if relay.PeekStreamID(b) != 2 {
		t.Fatalf("queued data frame stream = %d, want 2", relay.PeekStreamID(b))
	}
	if frameData(t, b) != "keep" {
		t.Fatalf("queued data = %q, want %q", frameData(t, b), "keep")
	}
	select {
	case b := <-sender.data:
		t.Fatalf("unexpected extra data frame: stream %d", relay.PeekStreamID(b))
	default:
	}

	select {
	case f := <-sender.ctrl:
		if f.Type != relay.FrameOpenOK || f.StreamID != 1 {
			t.Fatalf("ctrl frame = type 0x%02x stream %d, want open ok on stream 1", f.Type, f.StreamID)
		}
	default:
		t.Fatal("open ok not queued")
	}
}

func TestOutputThroughputConstants(t *testing.T) {
	if outputWindowBytes != 1024*1024 {
		t.Fatalf("outputWindowBytes = %d, want 1MiB", outputWindowBytes)
	}
	if outputReadBufBytes != 32*1024 {
		t.Fatalf("outputReadBufBytes = %d, want 32KiB", outputReadBufBytes)
	}
	if maxOutputFrameBytes >= relay.MaxWebSocketMessageBytes {
		t.Fatalf("maxOutputFrameBytes = %d must stay below ws message limit %d", maxOutputFrameBytes, relay.MaxWebSocketMessageBytes)
	}
	if maxOutputFrameBytes > outputWindowBytes {
		t.Fatalf("maxOutputFrameBytes = %d exceeds window %d", maxOutputFrameBytes, outputWindowBytes)
	}
}

func TestDrainIfGenChangedDropsLateStaleFrame(t *testing.T) {
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sender := newFrameSender()
	sr.appendOutput([]byte("AAAABBBB"))
	_ = sender.sendData(dataFrame(t, 9, 0, "other"))

	gen := sr.gen
	openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: 7}
	if err := sr.attachForOpen(4, sender, openOK); err != nil {
		t.Fatal(err)
	}
	if sr.gen == gen {
		t.Fatal("attach did not bump gen")
	}
	if err := sender.sendData(dataFrame(t, 7, 4, "BBBB")); err != nil {
		t.Fatal(err)
	}

	sr.drainIfGenChanged(gen, 7, sender)

	b := <-sender.data
	if relay.PeekStreamID(b) != 9 {
		t.Fatalf("surviving frame stream = %d, want 9", relay.PeekStreamID(b))
	}
	select {
	case b := <-sender.data:
		t.Fatalf("stale frame survived redrain: stream %d", relay.PeekStreamID(b))
	default:
	}
}

func TestDrainIfGenChangedKeepsFrameWhenGenSame(t *testing.T) {
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sender := newFrameSender()
	_ = sender.sendData(dataFrame(t, 7, 0, "AAAA"))

	sr.drainIfGenChanged(sr.gen, 7, sender)

	b := <-sender.data
	if frameData(t, b) != "AAAA" {
		t.Fatalf("frame data = %q, want %q", frameData(t, b), "AAAA")
	}
}

func TestRecoverLogRecoversGoroutinePanic(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer recoverLog(func() string { return "test pump" })
		panic("boom")
	}()
	<-done

	if !strings.Contains(buf.String(), "test pump panic: boom") {
		t.Fatalf("panic not logged: %q", buf.String())
	}
}

type lockedLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedLogBuf) count(sub string) int {
	return strings.Count(b.String(), sub)
}

func stalledRelay(t *testing.T, after, interval time.Duration) *streamRelay {
	t.Helper()
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sr.sidV.Store(42)
	sr.stallAfter = after
	sr.stallInterval = interval
	sr.appendOutput(make([]byte, outputWindowBytes))
	return sr
}

func captureStallLog(t *testing.T) *lockedLogBuf {
	t.Helper()
	out := &lockedLogBuf{}
	old := log.Writer()
	log.SetOutput(out)
	t.Cleanup(func() { log.SetOutput(old) })
	return out
}

func waitForStallLines(t *testing.T, out *lockedLogBuf, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if out.count("output stalled") >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stall log lines = %d, want >= %d: %q", out.count("output stalled"), want, out.String())
}

func TestWaitCreditLogsStallWithStreamDetails(t *testing.T) {
	out := captureStallLog(t)
	sr := stalledRelay(t, 30*time.Millisecond, 30*time.Millisecond)
	sr.markDetached()

	done := make(chan bool, 1)
	go func() { done <- sr.waitCredit(nil) }()

	waitForStallLines(t, out, 1)
	logs := out.String()
	if !strings.Contains(logs, "stream 42 output stalled") {
		t.Fatalf("log missing stream id: %q", logs)
	}
	if !strings.Contains(logs, "ack=0 ringEnd=1048576") {
		t.Fatalf("log missing ack/ringEnd: %q", logs)
	}
	if !strings.Contains(logs, "detachedSince=") {
		t.Fatalf("log missing detachedSince: %q", logs)
	}
	if strings.Contains(logs, "detachedSince=0001-01-01") {
		t.Fatalf("detachedSince not stamped: %q", logs)
	}
	sr.shutdown()
	if got := <-done; got {
		t.Fatal("waitCredit returned true after shutdown")
	}
}

func TestWaitCreditStallLogFrequency(t *testing.T) {
	out := captureStallLog(t)
	sr := stalledRelay(t, 30*time.Millisecond, 50*time.Millisecond)

	done := make(chan bool, 1)
	go func() { done <- sr.waitCredit(nil) }()

	waitForStallLines(t, out, 1)
	time.Sleep(220 * time.Millisecond)
	got := out.count("output stalled")
	if got < 3 || got > 8 {
		t.Fatalf("stall log lines = %d after ~4 intervals, want 3..8", got)
	}
	sr.shutdown()
	<-done
}

func TestWaitCreditNoStallLogWhenCreditReleased(t *testing.T) {
	out := captureStallLog(t)
	sr := stalledRelay(t, 80*time.Millisecond, 80*time.Millisecond)

	done := make(chan bool, 1)
	go func() { done <- sr.waitCredit(nil) }()

	time.Sleep(30 * time.Millisecond)
	sr.mu.Lock()
	sr.sent = outputWindowBytes
	sr.mu.Unlock()
	sr.setAck(outputWindowBytes)
	select {
	case got := <-done:
		if !got {
			t.Fatal("waitCredit returned false after ack")
		}
	case <-time.After(time.Second):
		t.Fatal("waitCredit did not return after ack")
	}
	time.Sleep(150 * time.Millisecond)
	if got := out.count("output stalled"); got != 0 {
		t.Fatalf("stall log lines = %d, want 0: %q", got, out.String())
	}
}

func TestWaitCreditStallReapsZeroAckStream(t *testing.T) {
	out := captureStallLog(t)
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sr.sidV.Store(42)
	sr.stallReap = 50 * time.Millisecond
	sr.appendOutput(make([]byte, outputWindowBytes))
	mgr.add(42, sr)

	done := make(chan bool, 1)
	go func() { done <- sr.waitCredit(mgr) }()

	select {
	case got := <-done:
		if got {
			t.Fatal("waitCredit returned true for stalled stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitCredit did not reap stalled stream")
	}
	if mgr.get(42) != nil {
		t.Fatal("stalled stream still registered")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed after stall reap")
	}
	logs := out.String()
	for _, want := range []string{"stream 42", "stall reaped", "ack=0", "ringEnd=1048576", "stalled="} {
		if !strings.Contains(logs, want) {
			t.Fatalf("reap log missing %q: %q", want, logs)
		}
	}
}

func TestWaitCreditStallReapResetByAckProgress(t *testing.T) {
	out := captureStallLog(t)
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sr.sidV.Store(7)
	sr.stallReap = 150 * time.Millisecond
	sr.appendOutput(make([]byte, outputWindowBytes))
	sr.mu.Lock()
	sr.sent = outputWindowBytes + 5*1024
	sr.mu.Unlock()
	mgr.add(7, sr)

	done := make(chan bool, 1)
	go func() { done <- sr.waitCredit(mgr) }()

	for i := 1; i <= 5; i++ {
		time.Sleep(40 * time.Millisecond)
		sr.appendOutput(make([]byte, 1024))
		sr.setAck(uint64(i * 1024))
	}
	select {
	case <-done:
		t.Fatal("waitCredit returned despite ack progress")
	default:
	}
	if mgr.get(7) == nil {
		t.Fatal("stream reaped despite ack progress")
	}
	if got := out.count("stall reaped"); got != 0 {
		t.Fatalf("stall reaped logged for progressing stream: %q", out.String())
	}
	sr.shutdown()
	if got := <-done; got {
		t.Fatal("waitCredit returned true after shutdown")
	}
}

func TestWaitCreditIdleStreamKeepsCredit(t *testing.T) {
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	sr.stallReap = 20 * time.Millisecond
	sr.appendOutput([]byte("small"))
	if !sr.waitCredit(newSessionManager()) {
		t.Fatal("waitCredit blocked idle stream")
	}
}
