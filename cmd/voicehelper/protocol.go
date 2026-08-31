package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// protocol 2 adds start_passthrough plus the audio/utterance_end events.
const protocolVersion = 2

type Command struct {
	Cmd  string `json:"cmd"`
	Path string `json:"path,omitempty"`
	// Kind selects the recognizer family for set_model: "sensevoice" (default
	// when empty), "sensevoice-int8", "paraformer". Protocol stays 2; old
	// clients never send it.
	Kind string `json:"kind,omitempty"`

	Threshold       *float64 `json:"threshold,omitempty"`
	MinSilence      *float64 `json:"min_silence,omitempty"`
	MinSpeech       *float64 `json:"min_speech,omitempty"`
	TrailingSilence *float64 `json:"trailing_silence,omitempty"`
	MaxSegment      *float64 `json:"max_segment,omitempty"`
	NoSpeechTimeout *float64 `json:"no_speech_timeout,omitempty"`
}

type Event struct {
	Type     string  `json:"type"`
	Version  string  `json:"version,omitempty"`
	Protocol int     `json:"protocol,omitempty"`
	Text     string  `json:"text,omitempty"`
	State    string  `json:"state,omitempty"`
	Msg      string  `json:"msg,omitempty"`
	Data     string  `json:"data,omitempty"`
	Pct      float64 `json:"pct,omitempty"`
}

type eventWriter struct {
	mu  sync.Mutex
	w   *bufio.Writer
	out io.Writer
}

func newEventWriter(out io.Writer) *eventWriter {
	return &eventWriter{w: bufio.NewWriter(out), out: out}
}

func (e *eventWriter) emit(ev Event) {
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.w.Write(line)
	e.w.WriteByte('\n')
	e.w.Flush()
}

func (e *eventWriter) state(s string)   { e.emit(Event{Type: "state", State: s}) }
func (e *eventWriter) partial(t string) { e.emit(Event{Type: "partial", Text: t}) }
func (e *eventWriter) final(t string)   { e.emit(Event{Type: "final", Text: t}) }

// infof reports transient setup progress (model downloads); unlike error it
// does not abort a settings-panel test recording on the app side.
func (e *eventWriter) infof(format string, args ...any) {
	e.emit(Event{Type: "info", Msg: fmt.Sprintf(format, args...)})
}
func (e *eventWriter) errorf(format string, args ...any) {
	e.emit(Event{Type: "error", Msg: fmt.Sprintf(format, args...)})
}
func (e *eventWriter) progress(pct float64) {
	e.emit(Event{Type: "download_progress", Pct: pct})
}

// audio emits one passthrough PCM chunk (16kHz mono S16LE, base64).
func (e *eventWriter) audio(pcm []byte) {
	e.emit(Event{Type: "audio", Data: base64.StdEncoding.EncodeToString(pcm)})
}
func (e *eventWriter) utteranceEnd() { e.emit(Event{Type: "utterance_end"}) }
