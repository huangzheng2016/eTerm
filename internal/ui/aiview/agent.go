package aiview

import "context"

type EventKind int

const (
	EventTextDelta EventKind = iota
	EventThinkingDelta
	EventToolCallStart
	EventToolCallEnd
	EventDone
	EventError
)

type AgentEvent struct {
	Kind EventKind
	Text string
}

type AgentRunner interface {
	Run(ctx context.Context, prompt string) (<-chan AgentEvent, error)
}

type Provider struct {
	Name    string
	Type    string
	BaseURL string
	APIKey  string
	Model   string
}

type ProviderStore interface {
	Providers() []Provider
	Active() string
	Switch(name string)
	Add(p Provider)
}
