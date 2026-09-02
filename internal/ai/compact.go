package ai

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// compactKeepTurns is how many recent turns stay verbatim after compaction.
const compactKeepTurns = 4

const compactSystemPrompt = `You are compacting the conversation between a user and an AI terminal assistant so it can continue within a limited context window.

Write a handoff summary of the transcript the user provides. Preserve:
- the user's goals and requirements
- decisions made, and why
- key facts discovered through tool calls (command output, file contents, host state)
- concrete file paths and host names
- tasks in progress and the immediate next step

Plain text with short section headings, at most about 800 words, in the same language as the conversation.`

const compactTranscriptPrefix = "Summarize this earlier part of the conversation (JSON messages):\n\n"

const compactSummaryPrefix = "[Earlier conversation compacted into this summary; the most recent turns follow verbatim.]\n\n"

// CompactStats reports the compaction effect for display.
type CompactStats struct {
	MessagesBefore int
	MessagesAfter  int
	TokensBefore   int
	TokensAfter    int
}

// Compact summarizes all but the last compactKeepTurns turns with the chat
// model and replaces the history with the summary plus those turns. It blocks
// on the run mutex, so it only takes effect between runs. On any model error
// the history is left untouched and the error is returned as-is.
func (a *Agent) Compact(ctx context.Context) (CompactStats, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.histMu.Lock()
	history := slices.Clone(a.history)
	a.histMu.Unlock()

	stats := CompactStats{
		MessagesBefore: len(history),
		TokensBefore:   int(countTokens(history, nil)),
	}
	start := lastTurnsStart(history, compactKeepTurns)
	if start == 0 {
		// Fewer turns than we keep verbatim: nothing to compact away.
		stats.MessagesAfter = stats.MessagesBefore
		stats.TokensAfter = stats.TokensBefore
		return stats, nil
	}

	transcript, err := json.Marshal(history[:start])
	if err != nil {
		return CompactStats{}, err
	}
	resp, err := a.chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(compactSystemPrompt),
		schema.UserMessage(compactTranscriptPrefix + string(transcript)),
	})
	if err != nil {
		return CompactStats{}, err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return CompactStats{}, errors.New("compact: model returned an empty summary")
	}

	compacted := make([]*schema.Message, 0, len(history)-start+1)
	compacted = append(compacted, schema.UserMessage(compactSummaryPrefix+strings.TrimSpace(resp.Content)))
	compacted = append(compacted, history[start:]...)

	a.histMu.Lock()
	a.history = compacted
	a.histMu.Unlock()

	stats.MessagesAfter = len(compacted)
	stats.TokensAfter = int(countTokens(compacted, nil))
	return stats, nil
}

// lastTurnsStart returns the index where the last keep turns begin, or 0 when
// the history holds no more than keep turns (nothing before them to compact).
func lastTurnsStart(msgs []*schema.Message, keep int) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.User {
			keep--
			if keep == 0 {
				return i
			}
		}
	}
	return 0
}
