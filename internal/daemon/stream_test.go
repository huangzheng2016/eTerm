package daemon

import (
	"bytes"
	"log"
	"strings"
	"testing"

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
