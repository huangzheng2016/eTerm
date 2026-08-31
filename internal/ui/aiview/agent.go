package aiview

import (
	"context"
	"time"
)

type EventKind int

const (
	EventTextDelta EventKind = iota
	EventThinkingDelta
	EventToolCallStart
	EventToolCallEnd
	EventDone
	EventError
	// EventSteer acks a queued message: it entered the agent's turn, so the
	// panel converts its dim queued block into a normal user block.
	EventSteer
)

type AgentEvent struct {
	Kind EventKind
	Text string
}

type AgentRunner interface {
	Run(ctx context.Context, prompt string) (<-chan AgentEvent, error)
	// Enqueue queues input submitted while a run is active; the agent
	// injects it at the next step boundary (acked with EventSteer).
	Enqueue(text string)
	// ClearQueue drops queued messages not yet injected (ctrl+c, ctrl+l).
	ClearQueue()
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

// SessionEntry describes one saved chat session for the /resume picker.
type SessionEntry struct {
	ID        string
	Title     string
	Provider  string
	Model     string
	UpdatedAt time.Time
}

// SessionStore persists chat sessions; implemented by the app bridge.
// SaveSession exports the agent history and upserts the row (create only when
// the history is non-empty; title and forkOf apply on create, a non-empty
// title also updates). LoadSession imports the session's history into the
// agent and returns it for block rebuilding.
type SessionStore interface {
	SaveSession(id, title, forkOf string)
	Sessions() []SessionEntry // ordered by updated_at desc
	LoadSession(id string) (history []byte, ok bool)
	UndoLastTurn()
	ResetHistory()
}
