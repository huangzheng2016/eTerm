package sshview

import (
	"io"
	"testing"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

type oneByteReader struct {
	data []byte
	pos  int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func TestReadLoopDoesNotDropChunksWhenChannelIsFull(t *testing.T) {
	m := &Model{
		sess: &internalssh.InteractiveSession{Stdout: &oneByteReader{data: []byte("abc")}},
		ch:   make(chan []byte, 1),
	}

	done := make(chan struct{})
	go func() {
		m.readLoop()
		close(done)
	}()

	var got []byte
	for b := range m.ch {
		got = append(got, b...)
	}
	<-done

	if string(got) != "abc" {
		t.Fatalf("got %q want %q", got, "abc")
	}
}
