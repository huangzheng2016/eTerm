package sshview

import (
	"os"
	"testing"
	"time"
)

func TestRecorderCapturesInputOutputAndResize(t *testing.T) {
	r := NewRecorder(time.Now())
	path := r.path
	r.Resize(24, 80)
	r.Input([]byte("secret\r"))
	r.Output([]byte("ok\r\n"))
	data, duration, stopped := r.Close()
	if len(data) == 0 || duration < 0 || stopped {
		t.Fatalf("data=%d duration=%v stopped=%v", len(data), duration, stopped)
	}
	if path != "" {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary replay file still exists: %v", err)
		}
	}
	events, err := DecodeReplay(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Kind != "r" || string(events[1].Data) != "secret\r" || string(events[2].Data) != "ok\r\n" {
		t.Fatalf("events=%+v", events)
	}
}

func TestRecorderStopsAt24Hours(t *testing.T) {
	r := NewRecorder(time.Now().Add(-MaxReplayDuration - time.Second))
	r.Output([]byte("discarded"))
	data, duration, stopped := r.Close()
	events, err := DecodeReplay(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || duration != MaxReplayDuration || !stopped {
		t.Fatalf("events=%+v duration=%v stopped=%v", events, duration, stopped)
	}
}
