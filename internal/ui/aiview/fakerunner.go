package aiview

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type FakeRunner struct {
	Events []AgentEvent
	Delay  time.Duration

	providers []Provider
	active    string

	// Queued collects Enqueue calls; a run acks each one with EventSteer at
	// the next event boundary, like the real agent's step-boundary injection.
	mu         sync.Mutex
	Queued     []string
	EnqueueErr error

	// In-memory SessionStore: History stands in for the agent's exported
	// history; sessions is ordered most-recent-first like the SQL query.
	History   []byte
	sessions  []fakeSession
	undoCalls int
	resets    int

	// TaskList stands in for the agent's background tasks; CancelTask flips
	// the matching entry to cancelled like the real TaskManager.
	TaskList       []TaskEntry
	cancelledTasks []string
}

type fakeSession struct {
	entry   SessionEntry
	history []byte
	forkOf  string
}

func NewFakeRunner() *FakeRunner {
	return &FakeRunner{
		Delay: 20 * time.Millisecond,
		providers: []Provider{
			{Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
			{Name: "anthropic", Type: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4"},
		},
		active: "openai",
	}
}

func (f *FakeRunner) Run(ctx context.Context, prompt string) (<-chan AgentEvent, error) {
	events := f.Events
	if events == nil {
		events = demoEvents(prompt)
	}
	// A real run grows the agent history; mirror that so SaveSession has
	// something to persist.
	f.History = []byte("turn")
	ch := make(chan AgentEvent, 64)
	go func() {
		defer close(ch)
		steered := 0
		for _, ev := range events {
			if f.Delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(f.Delay):
				}
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
			// Ack anything queued since the last event (step boundary).
			for {
				f.mu.Lock()
				if steered >= len(f.Queued) {
					f.mu.Unlock()
					break
				}
				text := f.Queued[steered]
				steered++
				f.mu.Unlock()
				select {
				case ch <- AgentEvent{Kind: EventSteer, Text: text}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (f *FakeRunner) Enqueue(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnqueueErr != nil {
		return f.EnqueueErr
	}
	f.Queued = append(f.Queued, text)
	return nil
}

func (f *FakeRunner) ClearQueue() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Queued = nil
}

func demoEvents(prompt string) []AgentEvent {
	return []AgentEvent{
		{Kind: EventThinkingDelta, Text: "The user asked: "},
		{Kind: EventThinkingDelta, Text: prompt},
		{Kind: EventTextDelta, Text: "Here is what I found:\n\n"},
		{Kind: EventTextDelta, Text: "```sh\nls -la\n```\n\n"},
		{Kind: EventToolCallStart, Text: "terminal"},
		{Kind: EventToolCallEnd, Text: "ls -la"},
		{Kind: EventTextDelta, Text: fmt.Sprintf("You said %q. This is a demo reply from the fake runner.", prompt)},
		{Kind: EventDone},
	}
}

func (f *FakeRunner) Models() []ModelEntry {
	out := make([]ModelEntry, 0, len(f.providers))
	for _, p := range f.providers {
		out = append(out, ModelEntry{Label: p.Name, Provider: p.Name, Model: p.Model, Type: p.Type})
	}
	return out
}

func (f *FakeRunner) Active() string { return f.active }

func (f *FakeRunner) Switch(provider, model string) {
	for _, p := range f.providers {
		if p.Name == provider {
			f.active = provider
			return
		}
	}
}

func (f *FakeRunner) Add(p Provider) {
	for i, existing := range f.providers {
		if existing.Name == p.Name {
			f.providers[i] = p
			return
		}
	}
	f.providers = append(f.providers, p)
}

func (f *FakeRunner) SaveSession(id, title, forkOf string) {
	for i := range f.sessions {
		if f.sessions[i].entry.ID == id {
			f.sessions[i].history = f.History
			if title != "" {
				f.sessions[i].entry.Title = title
			}
			e := f.sessions[i]
			f.sessions = append(f.sessions[:i], f.sessions[i+1:]...)
			f.sessions = append([]fakeSession{e}, f.sessions...)
			return
		}
	}
	if len(f.History) == 0 {
		return
	}
	f.sessions = append([]fakeSession{{
		entry:   SessionEntry{ID: id, Title: title, UpdatedAt: time.Now()},
		history: f.History,
		forkOf:  forkOf,
	}}, f.sessions...)
}

func (f *FakeRunner) Sessions() []SessionEntry {
	out := make([]SessionEntry, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, s.entry)
	}
	return out
}

func (f *FakeRunner) LoadSession(id string) ([]byte, bool) {
	for _, s := range f.sessions {
		if s.entry.ID == id {
			return s.history, true
		}
	}
	return nil, false
}

func (f *FakeRunner) UndoLastTurn() { f.undoCalls++ }

func (f *FakeRunner) ResetHistory() { f.resets++; f.History = nil }

func (f *FakeRunner) Tasks() []TaskEntry { return f.TaskList }

func (f *FakeRunner) CancelTask(id string) {
	f.cancelledTasks = append(f.cancelledTasks, id)
	for i := range f.TaskList {
		if f.TaskList[i].ID == id {
			f.TaskList[i].Status = "cancelled"
			f.TaskList[i].Tail = append(f.TaskList[i].Tail, TaskActivity{Kind: "status", Text: "cancelled"})
		}
	}
}
