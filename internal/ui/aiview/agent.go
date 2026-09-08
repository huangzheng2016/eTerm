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
	EventSteer
)

type AgentEvent struct {
	Kind     EventKind
	Text     string
	ToolName string
	ToolArgs string
}

type AgentRunner interface {
	Run(ctx context.Context, prompt string) (<-chan AgentEvent, error)
	Enqueue(text string) error
	ClearQueue()
	DequeueLast() (text string, ok bool)
	Compact(ctx context.Context) (CompactStats, error)
}

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

type ModelEntry struct {
	Label    string
	Provider string
	Model    string
	Type     string
}

type ProviderStore interface {
	Models() []ModelEntry
	Active() string
	Switch(provider, model string)
	Add(p Provider)
}

type TaskActivity struct {
	Kind string
	Text string
}

type TaskEntry struct {
	ID            string
	Task          string
	Status        string
	StartedSecAgo int
	Tail          []TaskActivity
}

type SessionEntry struct {
	ID        string
	Title     string
	Provider  string
	Model     string
	UpdatedAt time.Time
}

type SessionStore interface {
	SaveSession(id, title, forkOf string)
	Sessions() []SessionEntry
	LoadSession(id string) (history []byte, ok bool)
	UndoLastTurn()
	ResetHistory()
}
