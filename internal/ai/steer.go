package ai

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// steerPrefix marks queued user messages injected into a running turn.
const steerPrefix = "[steer] "

// steerQueue holds user messages submitted while a run is in flight. The
// steer middleware drains it at the next model call (step boundary); whatever
// is left when the turn ends is run as a chained turn by Agent.run.
type steerQueue struct {
	mu   sync.Mutex
	msgs []string
}

func (q *steerQueue) enqueue(text string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, text)
}

// drain takes all queued messages, preserving order.
func (q *steerQueue) drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.msgs) == 0 {
		return nil
	}
	msgs := q.msgs
	q.msgs = nil
	return msgs
}

// pop takes the oldest queued message.
func (q *steerQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.msgs) == 0 {
		return "", false
	}
	text := q.msgs[0]
	q.msgs = q.msgs[1:]
	return text, true
}

func (q *steerQueue) clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = nil
}

// steerMiddleware injects queued user messages into the running turn before
// each model call. eino v0.9.18 persists the returned state, so the appended
// messages are seen by this and all later iterations of the turn.
type steerMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	q *steerQueue
}

func newSteerMiddleware(q *steerQueue) *steerMiddleware {
	return &steerMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, q: q}
}

func (m *steerMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	for _, text := range m.q.drain() {
		msg := schema.UserMessage(steerPrefix + text)
		state.Messages = append(state.Messages, msg)
		// Surface the injection as an event at exactly this stream position:
		// the runner records it into history on its own goroutine in event
		// order, so the middleware never writes history from the ADK flow
		// goroutine (a user message between an assistant tool call and its
		// tool result breaks the pair for the API).
		_ = adk.SendEvent(ctx, &adk.AgentEvent{Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{Message: msg, Role: schema.User},
		}})
	}
	return ctx, state, nil
}
