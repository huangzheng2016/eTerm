package relay

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	in := Frame{
		Type:     FrameData,
		Flags:    7,
		StreamID: 42,
		Payload:  []byte("hello"),
	}

	out, err := Decode(Encode(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.Flags != in.Flags || out.StreamID != in.StreamID || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("got %#v want %#v", out, in)
	}
}

func TestResizePayloadRoundTrip(t *testing.T) {
	payload := ResizePayload(33, 120)
	rows, cols, err := ParseResize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 33 || cols != 120 {
		t.Fatalf("got %dx%d want 33x120", rows, cols)
	}
}

func TestDecodeRejectsShortFrame(t *testing.T) {
	if _, err := Decode([]byte{byte(FrameData)}); err == nil {
		t.Fatal("expected short frame error")
	}
}

func TestDecodeRejectsLengthMismatch(t *testing.T) {
	b := Encode(Frame{Type: FrameData, Payload: []byte("abc")})
	b[9] = 4
	if _, err := Decode(b); err == nil {
		t.Fatal("expected length mismatch error")
	}
}

func TestOpenOKCarriesPayload(t *testing.T) {
	in := Frame{Type: FrameOpenOK, StreamID: 7, Payload: []byte("ab12cd")}
	out, err := Decode(Encode(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != FrameOpenOK || out.StreamID != 7 || string(out.Payload) != "ab12cd" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestDataPayloadRoundTrip(t *testing.T) {
	payload := DataPayload(1<<40+3, []byte("hello"))
	seq, data, err := ParseData(payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1<<40+3 || string(data) != "hello" {
		t.Fatalf("got seq=%d data=%q", seq, data)
	}
}

func TestParseDataRejectsShortPayload(t *testing.T) {
	if _, _, err := ParseData([]byte("abc")); err == nil {
		t.Fatal("expected short data payload error")
	}
}

func TestAckPayloadRoundTrip(t *testing.T) {
	ack, err := ParseAck(AckPayload(262144))
	if err != nil {
		t.Fatal(err)
	}
	if ack != 262144 {
		t.Fatalf("got ack=%d", ack)
	}
	if _, err := ParseAck([]byte("abc")); err == nil {
		t.Fatal("expected short ack payload error")
	}
}
