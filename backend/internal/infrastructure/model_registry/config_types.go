package modelregistry

import (
	"strings"

	domainmodel "praxis/internal/domain/model"
)

type ProviderConfig struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	BaseURL     string            `json:"baseUrl"`
	ProxyURL    string            `json:"proxyUrl,omitempty"`
	Credential  *CredentialRecord `json:"credential,omitempty"`
	Models      []ModelConfig     `json:"models"`
}

type CredentialRecord struct {
	Type CredentialType `json:"type"`
	Key  string         `json:"key"`
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"

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

// DomainModel maps persisted configuration into the model domain.
func (c ModelConfig) DomainModel() (domainmodel.Model, error) {
	return domainmodel.NewModel(c.ID, c.ContextWindow, c.MaxOutputTokens, c.ReasoningLevels, c.DefaultReasoningLevel)
}

// ModelOption is the execution catalog exposed to desktop clients.
type ModelOption struct {
	GroupID               string
	ProviderID            string
	ModelID               string
	Label                 string
	ProviderName          string
	ReasoningLevels       []string
	DefaultReasoningLevel string
	AssignedAgents        []string
}

// RegistryConfig 是模型注册表的完整可编辑配置文档。
// 它描述配置语义，不绑定具体的文件存储实现。
type RegistryConfig struct {
	Groups    []GroupConfig    `json:"groups"`
	Providers []ProviderConfig `json:"providers"`
}

type modelKey struct {
	ProviderID string
	ModelID    string
}

// ContainsModel reports whether a model reference exists in a validated or
// candidate model configuration.
func ContainsModel(config RegistryConfig, providerID, modelID string) bool {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
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
