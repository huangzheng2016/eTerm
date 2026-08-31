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

// ModelEntry is one selectable final model: a model alias (kimi import) or a
// user-added provider with its raw model.
type ModelEntry struct {
	Label    string // unique display name: alias or provider name
	Provider string
	Model    string // alias or raw model id to activate
	Type     string
}

type ProviderStore interface {
	Models() []ModelEntry
	Active() string // Label of the active entry
	Switch(provider, model string)
	Add(p Provider)
}
