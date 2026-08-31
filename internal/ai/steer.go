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
	// onInject reports each message as it enters the turn (history record +
	// panel notification). Set by Agent.run for the duration of a run.
	onInject func(string)
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

func (q *steerQueue) setHook(f func(string)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.onInject = f
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
		state.Messages = append(state.Messages, schema.UserMessage(steerPrefix+text))
		m.q.mu.Lock()
		onInject := m.q.onInject
		m.q.mu.Unlock()
		if onInject != nil {
			onInject(text)
		}
	}
	return ctx, state, nil
}
