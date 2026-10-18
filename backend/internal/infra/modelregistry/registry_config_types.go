package modelregistry

import (
	"cmp"
	"errors"
	"fmt"
	modelconfig "praxis/internal/core/model/config"
	"slices"
)

// ProviderCredential 表示凭据文档中的一项 Provider 凭据。
type ProviderCredential struct {
	Type CredentialType `json:"type"`
	Key  string         `json:"key"`
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"

// ProviderCredentials 将稳定的 Provider ID 映射到凭据。
// 凭据与模型配置分开持久化，使 Provider 元数据的读取、编辑和共享不会暴露密钥。
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
