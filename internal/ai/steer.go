package ai

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const steerPrefix = "[steer] "

type steerQueue struct {
	mu   sync.Mutex
	msgs []string
}

func (q *steerQueue) enqueue(text string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, text)
}

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
		_ = adk.SendEvent(ctx, &adk.AgentEvent{Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{Message: msg, Role: schema.User},
		}})
	}
	return ctx, state, nil
}
