// Package voice provides speech-to-text engines for voice input: a local
// engine driving the voicehelper subprocess (sherpa-onnx VAD+ASR) and a
// Volcano Engine cloud engine. Pure Go, no cgo, no UI imports.
package voice

import "context"

const (
	EventPartial          = "partial"
	EventFinal            = "final"
	EventState            = "state"
	EventError            = "error"
	EventDownloadProgress = "download_progress"
)

// Engine states reported in Event.State.
const (
	StateIdle      = "idle"
	StateListening = "listening"
	StateSpeech    = "speech"
	StateSilence   = "silence"
)

// Event is one message from an engine.
type Event struct {
	Type  string
	Text  string  // partial/final text
	State string  // state events
	Msg   string  // error events
	Pct   float64 // download progress, percent
}

// VADParams tunes endpoint detection. Zero fields keep engine defaults.
type VADParams struct {
	Threshold       float64 // speech probability threshold, 0..1
	MinSilence      float64 // seconds of silence to split segments
	MinSpeech       float64 // seconds of speech to keep a segment
	TrailingSilence float64 // seconds of silence after speech to finalize
	MaxSegment      float64 // seconds; force-finalize long speech
	NoSpeechTimeout float64 // seconds waiting for speech before cancel
}

// Engine is a speech-to-text session.
type Engine interface {
	// Start begins a listening session.
	Start(ctx context.Context) error
	// Stop ends the session; pending speech is finalized.
	Stop() error
	// SetVAD updates endpoint detection parameters.
	SetVAD(p VADParams) error
	// Events streams engine events; closed after Close.
	Events() <-chan Event
	// Close releases all resources.
	Close() error
}

// SentenceEnd is the action appended to a finalized sentence on delivery.
type SentenceEnd string

const (
	SentenceEndEnter SentenceEnd = "enter"
	SentenceEndSpace SentenceEnd = "space"
)

// Apply returns the finalized text with the sentence-end suffix.
func (s SentenceEnd) Apply(text string) string {
	switch s {
	case SentenceEndEnter:
		return text + "\n"
	case SentenceEndSpace:
		return text + " "
	default:
		return text
	}
}
