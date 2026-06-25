package daemon

import (
	"bytes"
	"strings"
	"testing"
)

func TestRingBufferBelowCap(t *testing.T) {
	r := newRingBuffer()
	r.Write([]byte("hello"))
	r.Write([]byte(" world"))
	if got := string(r.Bytes()); got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestRingBufferWrap(t *testing.T) {
	r := newRingBuffer()
	first := bytes.Repeat([]byte("a"), ringCap)
	r.Write(first)
	r.Write([]byte("xyz"))
	got := r.Bytes()
	if len(got) != ringCap {
		t.Fatalf("len = %d, want %d", len(got), ringCap)
	}
	if string(got[ringCap-3:]) != "xyz" {
		t.Fatalf("tail = %q", got[ringCap-3:])
	}
	if got[0] != 'a' {
		t.Fatalf("head = %q", got[0])
	}
	if want := ringCap - 3; !bytes.Equal(got[:want], bytes.Repeat([]byte("a"), want)) {
		t.Fatalf("unexpected leading bytes")
	}
}

func TestRingBufferSingleWriteOverCap(t *testing.T) {
	r := newRingBuffer()
	big := []byte(strings.Repeat("a", ringCap-1) + strings.Repeat("b", ringCap+5))
	r.Write(big)
	got := r.Bytes()
	if len(got) != ringCap {
		t.Fatalf("len = %d, want %d", len(got), ringCap)
	}
	if !bytes.Equal(got, big[len(big)-ringCap:]) {
		t.Fatalf("ring kept wrong tail")
	}
}

func TestRingBufferEmpty(t *testing.T) {
	r := newRingBuffer()
	if len(r.Bytes()) != 0 {
		t.Fatalf("expected empty")
	}
}
