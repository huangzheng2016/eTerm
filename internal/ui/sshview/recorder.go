package sshview

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"sync"
	"time"
)

const MaxReplayDuration = 24 * time.Hour

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
	buf     bytes.Buffer
	zip     *gzip.Writer
	enc     *json.Encoder
}

func NewRecorder(start time.Time) *Recorder {
	r := &Recorder{start: start}
	r.zip = gzip.NewWriter(&r.buf)
	r.enc = json.NewEncoder(r.zip)
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
	_ = r.enc.Encode(event)
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
		r.closed = true
	}
	return append([]byte(nil), r.buf.Bytes()...), r.last, r.stopped
}

func DecodeReplay(data []byte) ([]ReplayEvent, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	dec := json.NewDecoder(zr)
	var events []ReplayEvent
	for {
		var event ReplayEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				return events, nil
			}
			return nil, err
		}
		events = append(events, event)
	}
}
