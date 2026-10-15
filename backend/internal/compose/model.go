package compose

import (
	"fmt"
	"net/http"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
)

func newModelStream(
	format modelconfig.APIFormat,
	provider modelconfig.Provider,
	configured modelconfig.Model,
	apiKey string,
	client *http.Client,
) (model.ModelStream, error) {
	switch format {
	case modelconfig.APIFormatAnthropicMessages:
		return anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	case modelconfig.APIFormatOpenAIChatCompletions:
		return openaichat.New(openaichat.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	case modelconfig.APIFormatOpenAIResponses:
		return openairesponses.New(openairesponses.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			HTTPClient:    client,
			ContextWindow: configured.ContextWindow,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", format)
	}
}
