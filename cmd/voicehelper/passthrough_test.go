package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func readEvents(t *testing.T, buf *bytes.Buffer) []Event {
	t.Helper()
	var out []Event
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad event line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func TestPCMBytesRoundTrip(t *testing.T) {
	samples := []float32{0, 0.5, -0.5, 1.0, -1.0}
	b := pcmBytes(samples)
	if len(b) != 2*len(samples) {
		t.Fatalf("len = %d", len(b))
	}
	for i, s := range samples {
		got := int16(binary.LittleEndian.Uint16(b[2*i:]))
		want := int16(s * 32767)
		if got != want {
			t.Fatalf("sample %d: got %d want %d", i, got, want)
		}
	}
}

func TestAudioEventShape(t *testing.T) {
	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	pcm := []byte{0x01, 0x02, 0x03}
	ev.audio(pcm)

	evs := readEvents(t, &buf)
	if len(evs) != 1 || evs[0].Type != "audio" {
		t.Fatalf("events = %+v", evs)
	}
	data, err := base64.StdEncoding.DecodeString(evs[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pcm) {
		t.Fatalf("data = %v", data)
	}
}

// stop in passthrough mode marks utterance end instead of decoding.
func TestPassthroughStopEmitsUtteranceEnd(t *testing.T) {
	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	eng := newASREngine(ev, t.TempDir())
	eng.passthrough = true
	eng.state = "speech"
	eng.speechSeen = true

	eng.stop()

	evs := readEvents(t, &buf)
	if len(evs) != 2 || evs[0].Type != "utterance_end" || evs[1].Type != "state" || evs[1].State != "idle" {
		t.Fatalf("events = %+v", evs)
	}
}

// stop without speech stays quiet (no utterance_end, no final).
func TestPassthroughStopWithoutSpeech(t *testing.T) {
	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	eng := newASREngine(ev, t.TempDir())
	eng.passthrough = true
	eng.state = "listening"

	eng.stop()

	evs := readEvents(t, &buf)
	if len(evs) != 1 || evs[0].Type != "state" || evs[0].State != "idle" {
		t.Fatalf("events = %+v", evs)
	}
}

// finalize in passthrough mode emits utterance_end and keeps listening.
func TestPassthroughFinalizeEmitsUtteranceEnd(t *testing.T) {
	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	eng := newASREngine(ev, t.TempDir())
	eng.passthrough = true
	eng.state = "speech"
	eng.speechSeen = true

	eng.finalize()

	evs := readEvents(t, &buf)
	if len(evs) != 2 || evs[0].Type != "utterance_end" || evs[1].Type != "state" || evs[1].State != "listening" {
		t.Fatalf("events = %+v", evs)
	}
}

// ASR mode is unchanged: stop emits the accumulated text as final.
func TestASRStopStillEmitsFinal(t *testing.T) {
	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	eng := newASREngine(ev, t.TempDir())
	eng.state = "speech"
	eng.speechSeen = true
	eng.accumText = "hello"

	eng.stop()

	evs := readEvents(t, &buf)
	if len(evs) != 2 || evs[0].Type != "final" || evs[0].Text != "hello" || evs[1].State != "idle" {
		t.Fatalf("events = %+v", evs)
	}
}
