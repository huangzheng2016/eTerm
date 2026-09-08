package voice

import "context"

const (
	EventPartial          = "partial"
	EventFinal            = "final"
	EventState            = "state"
	EventError            = "error"
	EventInfo             = "info"
	EventDownloadProgress = "download_progress"
)

const (
	StateIdle      = "idle"
	StateListening = "listening"
	StateSpeech    = "speech"
	StateSilence   = "silence"
)

type Event struct {
	Type  string
	Text  string
	State string
	Msg   string
	Pct   float64
}

type VADParams struct {
	Threshold       float64
	MinSilence      float64
	MinSpeech       float64
	TrailingSilence float64
	MaxSegment      float64
	NoSpeechTimeout float64
}

type Engine interface {
	Start(ctx context.Context) error
	Stop() error
	SetVAD(p VADParams) error
	SetModel(dir, kind string) error
	Events() <-chan Event
	Close() error
}

type SentenceEnd string

const (
	SentenceEndEnter SentenceEnd = "enter"
	SentenceEndSpace SentenceEnd = "space"
)

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
