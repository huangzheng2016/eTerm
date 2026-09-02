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
	// ToolName/ToolArgs carry the tool call's name and raw input on
	// EventToolCallStart; when ToolName is empty, Text is the display name.
	ToolName string
	ToolArgs string
}

type AgentRunner interface {
	Run(ctx context.Context, prompt string) (<-chan AgentEvent, error)
	// Enqueue queues input submitted while a run is active; the agent
	// injects it at the next step boundary (acked with EventSteer). It
	// fails when there is nothing to queue onto.
	Enqueue(text string) error
	// ClearQueue drops queued messages not yet injected (ctrl+c, ctrl+l).
	ClearQueue()
	// DequeueLast removes the newest queued (not yet injected) message for
	// queue recall; ok is false when nothing is queued.
	DequeueLast() (text string, ok bool)
	// Compact summarizes older history with the chat model and reports the
	// before/after message and estimated token counts.
	Compact(ctx context.Context) (CompactStats, error)
}

// CompactStats reports a compaction's effect for display.
type CompactStats struct {
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int
	TokensAfter    int
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

// TaskActivity is one entry in a background task's activity tail: a text
// snippet, a tool call summary, or a status transition.
type TaskActivity struct {
	Kind string // text | tool | status
	Text string
}

// TaskEntry describes one background sub-agent for the /tasks view.
type TaskEntry struct {
	ID            string
	Task          string
	Status        string // running | done | error | cancelled
	StartedSecAgo int
	Tail          []TaskActivity // oldest first
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
