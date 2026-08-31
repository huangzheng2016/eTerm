package ai

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

const defaultClaudeMaxTokens = 8192

func NewChatModel(ctx context.Context, p *Provider, modelName string) (model.ChatModel, error) {
	switch p.Type {
	case ProviderOpenAI:
		cfg := &openai.ChatModelConfig{
			Model:  modelName,
			APIKey: p.APIKey,
		}
		if p.BaseURL != "" {
			cfg.BaseURL = p.BaseURL
		}
		return openai.NewChatModel(ctx, cfg)
	case ProviderClaude:
		cfg := &claude.Config{
			APIKey:    p.APIKey,
			Model:     modelName,
			MaxTokens: defaultClaudeMaxTokens,
		}
		if p.BaseURL != "" {
			cfg.BaseURL = &p.BaseURL
		}
		return claude.NewChatModel(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", p.Type)
	}
}
