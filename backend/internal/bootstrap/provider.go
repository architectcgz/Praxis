package bootstrap

import (
	"fmt"
	"net/http"
	modelapi "praxis/internal/model"
	modelconfig "praxis/internal/model/config"
	anthropicmessages "praxis/internal/model/provider/anthropic"
	"praxis/internal/model/provider/openaichat"
	"praxis/internal/model/provider/openairesponses"
)

func newModelStream(
	format modelconfig.APIFormat,
	provider modelconfig.Provider,
	configured modelconfig.Model,
	apiKey string,
	client *http.Client,
) (modelapi.ModelStream, error) {
	switch format {
	case modelconfig.APIFormatAnthropicMessages:
		return anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			ContextWindow: configured.ContextWindow,
		}, anthropicmessages.Dependencies{
			HTTPClient: client,
		})
	case modelconfig.APIFormatOpenAIChatCompletions:
		return openaichat.New(openaichat.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			ContextWindow: configured.ContextWindow,
		}, openaichat.Dependencies{
			HTTPClient: client,
		})
	case modelconfig.APIFormatOpenAIResponses:
		return openairesponses.New(openairesponses.Config{
			BaseURL:       provider.BaseURL,
			APIKey:        apiKey,
			ContextWindow: configured.ContextWindow,
		}, openairesponses.Dependencies{
			HTTPClient: client,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", format)
	}
}
