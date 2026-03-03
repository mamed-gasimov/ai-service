package openai

import (
	"context"
	"fmt"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/mamed-gasimov/ai-service/internal/modules/translation"
)

var _ translation.Translator = (*Translator)(nil)

type Translator struct {
	client openaisdk.Client
}

func NewTranslator(apiKey, baseURL string) *Translator {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Translator{client: openaisdk.NewClient(opts...)}
}

func (t *Translator) Translate(ctx context.Context, input string) (string, error) {
	resp, err := t.client.Chat.Completions.New(ctx, openaisdk.ChatCompletionNewParams{
		Model: openaisdk.ChatModelGPT4oMini,
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			openaisdk.SystemMessage(
				"You are a translation and summarization assistant. " +
					"Read the provided file content, which may be in any language. " +
					"Respond ONLY in English. " +
					"Write exactly 2-3 sentences that summarize the key purpose and main points of the content. " +
					"Do not include any preamble, explanation, or additional commentary.",
			),
			openaisdk.UserMessage(input),
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from openai")
	}
	return resp.Choices[0].Message.Content, nil
}
