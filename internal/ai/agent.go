package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultMaxIterations  = 32
	defaultContextWindow  = 131072
	clearThresholdRatio   = 0.75
	compactThresholdRatio = 0.90
)

func buildADKAgent(ctx context.Context, model einomodel.ChatModel, tools []tool.BaseTool, instruction string, maxIterations, contextWindow int) (*adk.ChatModelAgent, error) {
	patchMw, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create patchtoolcalls middleware: %w", err)
	}

	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	reduceMw, err := reduction.New(ctx, &reduction.Config{
		SkipTruncation: true,
		TokenCounter: func(_ context.Context, msgs []adk.Message, tools []*schema.ToolInfo) (int64, error) {
			return countTokens(msgs, tools), nil
		},
		MaxTokensForClear:         int64(float64(contextWindow) * clearThresholdRatio),
		ClearRetentionSuffixLimit: 2,
	})
	if err != nil {
		return nil, fmt.Errorf("create reduction middleware: %w", err)
	}

	summaryMw, err := summarization.New(ctx, &summarization.Config{
		Model: model,
		TokenCounter: func(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
			return int(countTokens(input.Messages, input.Tools)), nil
		},
		Trigger: &summarization.TriggerCondition{ContextTokens: int(float64(contextWindow) * compactThresholdRatio)},
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization middleware: %w", err)
	}

	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "eterm-agent",
		Instruction: instruction,
		Model:       model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: maxIterations,
		Handlers: []adk.ChatModelAgentMiddleware{
			patchMw,
			reduceMw,
			summaryMw,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model agent: %w", err)
	}
	return agent, nil
}

// estimateTokens: ASCII text ~ 4 chars/token, CJK text ~ 1.5 chars/token.
func estimateTokens(s string) int64 {
	var ascii, nonAscii int
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			nonAscii++
		}
	}
	return int64(float64(ascii)/4.0 + float64(nonAscii)/1.5)
}

func countTokens(msgs []adk.Message, tools []*schema.ToolInfo) int64 {
	var tokens int64
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		tokens += estimateTokens(msg.Content)
		tokens += estimateTokens(msg.ReasoningContent)
		if msg.Role == schema.Assistant {
			for _, tc := range msg.ToolCalls {
				tokens += estimateTokens(tc.Function.Name)
				tokens += estimateTokens(tc.Function.Arguments)
			}
		}
	}
	for _, t := range tools {
		text, _ := json.Marshal(t)
		tokens += estimateTokens(string(text))
	}
	return tokens
}
