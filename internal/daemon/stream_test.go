package daemon

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

func TestDrainDataDropsOnlyTargetStream(t *testing.T) {
	s := newFrameSender()
	mk := func(id uint32, data string) relay.Frame {
		return relay.Frame{Type: relay.FrameData, StreamID: id, Payload: []byte(data)}
	}
	_ = s.send(mk(1, "a"))
	_ = s.send(mk(2, "b"))
	_ = s.send(mk(1, "c"))
	_ = s.send(mk(2, "d"))

	s.drainData(1)

	var got []relay.Frame
	for {
		select {
		case f := <-s.data:
			got = append(got, f)
		default:
			if len(got) != 2 {
				t.Fatalf("queued frames = %d, want 2", len(got))
			}
			for _, f := range got {
				if f.StreamID != 2 {
					t.Fatalf("frame for stream %d survived drain of stream 1", f.StreamID)
				}
			}
			if string(got[0].Payload) != "b" || string(got[1].Payload) != "d" {
				t.Fatalf("kept payloads = %q,%q", got[0].Payload, got[1].Payload)
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
	_ = sender.send(relay.Frame{Type: relay.FrameData, StreamID: 1, Payload: relay.DataPayload(0, []byte("drop"))})
	_ = sender.send(relay.Frame{Type: relay.FrameData, StreamID: 2, Payload: relay.DataPayload(0, []byte("keep"))})

	openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: 1}
	if err := sr1.attachClamped(1, 0, sender, openOK); err != nil {
		t.Fatal(err)
	}
	if got := sr1.sidV.Load(); got != 1 {
		t.Fatalf("sidV = %d, want 1", got)
	}

	f := <-sender.data
	if f.StreamID != 2 {
		t.Fatalf("queued data frame stream = %d, want 2", f.StreamID)
	}
	_, data, err := relay.ParseData(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("queued data = %q, want %q", data, "keep")
	}
	select {
	case f := <-sender.data:
		t.Fatalf("unexpected extra data frame: stream %d", f.StreamID)
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
