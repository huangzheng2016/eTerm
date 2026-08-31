package aiview

import (
	"context"
	"fmt"
	"time"
)

type FakeRunner struct {
	Events []AgentEvent
	Delay  time.Duration

	providers []Provider
	active    string
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
	ch := make(chan AgentEvent, 64)
	go func() {
		defer close(ch)
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
		}
	}()
	return ch, nil
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
