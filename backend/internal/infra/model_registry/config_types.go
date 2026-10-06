package modelregistry

import (
	"errors"
	"fmt"
	"strings"
)

type ProviderConfig struct {
	ID             string        `json:"id"`
	DisplayName    string        `json:"displayName"`
	BaseURL        string        `json:"baseUrl"`
	ProxyURL       string        `json:"proxyUrl,omitempty"`
	DefaultModelID string        `json:"defaultModelId,omitempty"`
	Models         []ModelConfig `json:"models"`
}

// ProviderCredential is one Provider credential in the auth document.
type ProviderCredential struct {
	Type CredentialType `json:"type"`
	Key  string         `json:"key"`
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"

// ProviderCredentials maps stable Provider IDs to their credentials. It is
// persisted separately from the model configuration so provider metadata can
// be read, edited, or shared without ever exposing a secret.
type ProviderCredentials map[string]ProviderCredential

type GroupConfig struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type ModelAPIFormat string

const (
	APIFormatAnthropicMessages     ModelAPIFormat = "anthropic_messages"
	APIFormatOpenAIResponses       ModelAPIFormat = "openai_responses"
	APIFormatOpenAIChatCompletions ModelAPIFormat = "openai_chat_completions"
)

type ModelConfig struct {
	ID                    string         `json:"id"`
	DisplayName           string         `json:"displayName"`
	GroupID               string         `json:"groupId"`
	APIFormat             ModelAPIFormat `json:"apiFormat"`
	ContextWindow         int            `json:"contextWindow"`
	MaxOutputTokens       int            `json:"maxOutputTokens"`
	ReasoningLevels       []string       `json:"reasoningLevels,omitempty"`
	DefaultReasoningLevel string         `json:"defaultReasoningLevel,omitempty"`
}

// normalizeCapabilities validates the capability limits and canonicalizes the
// reasoning configuration in place, so the persisted document matches what the
// runtime resolves.
func (c *ModelConfig) normalizeCapabilities() error {
	levels, defaultLevel, err := normalizeReasoningLevels(c.ReasoningLevels, c.DefaultReasoningLevel)
	if err != nil {
		return err
	}
	if c.ContextWindow <= 0 || c.MaxOutputTokens <= 0 || c.MaxOutputTokens >= c.ContextWindow {
		return errors.New("model capability limits are invalid")
	}
	c.ReasoningLevels = levels
	c.DefaultReasoningLevel = defaultLevel
	return nil
}

// selectReasoning 解析推理等级，生成 Task 绑定的冻结模型选择。
func (c ModelConfig) selectReasoning(providerID, reasoningLevel string) (modelSelection, error) {
	if len(c.ReasoningLevels) == 0 {
		if reasoningLevel != "" {
			return modelSelection{}, fmt.Errorf("model %q does not support reasoning", c.ID)
		}
		return newModelSelection(providerID, c.ID, "")
	}
	if reasoningLevel == "" {
		reasoningLevel = c.DefaultReasoningLevel
	}
	if !containsReasoningLevel(c.ReasoningLevels, reasoningLevel) {
		return modelSelection{}, fmt.Errorf("model %q does not support reasoning %q", c.ID, reasoningLevel)
	}
	return newModelSelection(providerID, c.ID, reasoningLevel)
}

func normalizeReasoningLevels(reasoningLevels []string, defaultReasoningLevel string) ([]string, string, error) {
	if len(reasoningLevels) == 0 {
		if strings.TrimSpace(defaultReasoningLevel) != "" {
			return nil, "", errors.New("reasoning default requires levels")
		}
		return nil, "", nil
	}
	levels := make([]string, 0, len(reasoningLevels))
	seen := make(map[string]struct{}, len(reasoningLevels))
	for _, level := range reasoningLevels {
		level = strings.TrimSpace(level)
		if level == "" {
			return nil, "", errors.New("reasoning levels cannot be empty")
		}
		if _, exists := seen[level]; exists {
			return nil, "", fmt.Errorf("duplicate reasoning level %q", level)
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}
	defaultLevel := strings.TrimSpace(defaultReasoningLevel)
	if defaultLevel == "" {
		defaultLevel = defaultReasoningLevel
	}
	if !containsReasoningLevel(levels, defaultLevel) {
		return nil, "", fmt.Errorf("reasoning default %q is not supported", defaultLevel)
	}
	return levels, defaultLevel, nil
}

func containsReasoningLevel(levels []string, target string) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

// ModelOption 是桌面客户端可选择的模型条目。
type ModelOption struct {
	GroupID                  string
	ProviderID               string
	ModelID                  string
	DefaultProviderID        string
	DefaultModelID           string
	Label                    string
	ProviderName             string
	ReasoningLevels          []string
	DefaultReasoningLevel    string
	AssignedAgentDefinitions []string
}

type RegistryConfig struct {
	Groups            []GroupConfig    `json:"groups"`
	DefaultProviderID string           `json:"defaultProviderId"`
	Providers         []ProviderConfig `json:"providers"`
}

type modelKey struct {
	ProviderID string
	ModelID    string
}

// ContainsModel reports whether a model reference exists in a validated or
// candidate model configuration.
func ContainsModel(config RegistryConfig, providerID, modelID string) bool {
	for _, provider := range config.Providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID {
				return true
			}
		}
	}
	return false
}

func cloneModels(models []ModelConfig) []ModelConfig {
	copy := append([]ModelConfig(nil), models...)
	for index := range copy {
		copy[index].ReasoningLevels = append([]string(nil), models[index].ReasoningLevels...)
	}
	return copy
}

type modelSelection struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

func newModelSelection(providerID, modelID, reasoningLevel string) (modelSelection, error) {
	selection := modelSelection{
		ProviderID:     providerID,
		ModelID:        modelID,
		ReasoningLevel: reasoningLevel,
	}
	if selection.ProviderID == "" {
		return modelSelection{}, errors.New("model selection provider ID is required")
	}
	if selection.ModelID == "" {
		return modelSelection{}, errors.New("model selection model ID is required")
	}
	return selection, nil
}
