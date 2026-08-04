package sshview

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

const MaxReplayDuration = 48 * time.Hour

type ReplayEvent struct {
	At   int64  `json:"t"`
	Kind string `json:"k"`
	Data []byte `json:"d,omitempty"`
	Rows int    `json:"r,omitempty"`
	Cols int    `json:"c,omitempty"`
}

type Recorder struct {
	mu      sync.Mutex
	start   time.Time
	last    time.Duration
	stopped bool
	closed  bool
	file    *os.File
	path    string
	buf     bytes.Buffer
	zip     *zstd.Encoder
	lastAt  int64
}

func NewRecorder(start time.Time) *Recorder {
	r := &Recorder{start: start}
	var dst io.Writer = &r.buf
	if file, err := os.CreateTemp("", "eterm-replay-*.zst"); err == nil {
		r.file = file
		r.path = file.Name()
		dst = file
	}
	var err error
	r.zip, err = zstd.NewWriter(dst)
	if err != nil {
		if r.file != nil {
			_ = r.file.Close()
			_ = os.Remove(r.path)
			r.file = nil
			r.path = ""
		}
		r.zip, _ = zstd.NewWriter(&r.buf)
	}
	_, _ = r.zip.Write([]byte("ETR2"))
	return r
}

func (r *Recorder) Input(data []byte)  { r.record(ReplayEvent{Kind: "i", Data: data}) }
func (r *Recorder) Output(data []byte) { r.record(ReplayEvent{Kind: "o", Data: data}) }
func (r *Recorder) Resize(rows, cols int) {
	r.record(ReplayEvent{Kind: "r", Rows: rows, Cols: cols})
}

func (r *Recorder) record(event ReplayEvent) {
	if r == nil || len(event.Data) == 0 && event.Kind != "r" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.stopped {
		return
	}
	elapsed := time.Since(r.start)
	if elapsed >= MaxReplayDuration {
		r.last = MaxReplayDuration
		r.stopped = true
		return
	}
	r.last = elapsed
	event.At = elapsed.Milliseconds()
	delta := event.At - r.lastAt
	if delta < 0 {
		delta = 0
	}
	r.lastAt = event.At
	var header [3*binary.MaxVarintLen64 + 1]byte
	n := binary.PutUvarint(header[:], uint64(delta))
	header[n] = event.Kind[0]
	n++
	if event.Kind == "r" {
		n += binary.PutUvarint(header[n:], uint64(event.Rows))
		n += binary.PutUvarint(header[n:], uint64(event.Cols))
	} else {
		n += binary.PutUvarint(header[n:], uint64(len(event.Data)))
	}
	_, _ = r.zip.Write(header[:n])
	if event.Kind != "r" {
		_, _ = r.zip.Write(event.Data)
	}
}

func (r *Recorder) Close() ([]byte, time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.last = time.Since(r.start)
		if r.last >= MaxReplayDuration {
			r.last = MaxReplayDuration
			r.stopped = true
		}
		_ = r.zip.Close()
		if r.file != nil {
			_ = r.file.Sync()
			_ = r.file.Close()
			if data, err := os.ReadFile(r.path); err == nil {
				r.buf.Write(data)
			}
			_ = os.Remove(r.path)
		}
		r.closed = true
	}
	return r.buf.Bytes(), r.last, r.stopped
}

func decodeReplayBinary(data []byte) ([]ReplayEvent, error) {
	if len(data) < 4 || string(data[:4]) != "ETR2" {
		return nil, fmt.Errorf("invalid replay format")
	}
	data = data[4:]
	var events []ReplayEvent
	var at int64
	for len(data) > 0 {
		delta, n := binary.Uvarint(data)
		if n <= 0 {
			return nil, fmt.Errorf("invalid replay timestamp")
		}
		data = data[n:]
		if len(data) < 1 {
			return nil, fmt.Errorf("invalid replay event")
		}
		kind := string(data[0])
		data = data[1:]
		v, n := binary.Uvarint(data)
		if n <= 0 {
			return nil, fmt.Errorf("invalid replay payload")
		}
		data = data[n:]
		event := ReplayEvent{At: at + int64(delta), Kind: kind}
		at = event.At
		if kind == "r" {
			event.Rows = int(v)
			v, n = binary.Uvarint(data)
			if n <= 0 {
				return nil, fmt.Errorf("invalid replay resize")
			}
			data = data[n:]
			event.Cols = int(v)
		} else if kind == "i" || kind == "o" {
			if v > uint64(len(data)) {
				return nil, fmt.Errorf("invalid replay data length")
			}
			event.Data = append([]byte(nil), data[:int(v)]...)
			data = data[int(v):]
		} else {
			return nil, fmt.Errorf("invalid replay event kind")
		}
		events = append(events, event)
	}
	return events, nil
}

func DecodeReplay(data []byte) ([]ReplayEvent, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return decodeReplayBinary(decoded)
}
