package modelregistry

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	modelconfig "praxis/internal/core/model/config"
)

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

// selectReasoning 解析推理等级，生成 Task 绑定的冻结模型选择。
func selectReasoning(c modelconfig.Model, providerID, reasoningLevel string) (modelSelection, error) {
	if len(c.ReasoningLevels) == 0 {
		if reasoningLevel != "" {
			return modelSelection{}, fmt.Errorf("model %q does not support reasoning", c.ID)
		}
		return newModelSelection(providerID, c.ID, "")
	}
	reasoningLevel = cmp.Or(reasoningLevel, c.DefaultReasoningLevel)
	if !slices.Contains(c.ReasoningLevels, reasoningLevel) {
		return modelSelection{}, fmt.Errorf("model %q does not support reasoning %q", c.ID, reasoningLevel)
	}
	return newModelSelection(providerID, c.ID, reasoningLevel)
}

type modelKey struct {
	ProviderID string
	ModelID    string
}

// ContainsModel 判断模型引用是否存在于已校验配置或候选配置中。
func ContainsModel(config modelconfig.Config, providerID, modelID string) bool {
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
