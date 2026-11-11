package model

import (
	"fmt"
	modelconfig "praxis/internal/core/model/config"
	"slices"
	"strings"
)

// Catalog 保存加载后的只读模型目录，不包含凭据或存储能力。
// 只接收输入边界已校验的配置，加载时准备各个已配置推理等级的完整选项。
// 模型配置查询直接返回已准备的只读配置，不重新校验或覆盖参数。
type Catalog struct {
	options []modelconfig.Option
	models  map[modelKey]Config
}

type modelKey struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

func NewCatalog(config modelconfig.Config) *Catalog {
	catalog := &Catalog{
		options: make([]modelconfig.Option, 0),
		models:  make(map[modelKey]Config),
	}
	for _, provider := range config.Providers {
		for _, candidate := range provider.Models {
			prepared := Config{
				ProviderID:      provider.ID,
				ModelID:         candidate.ID,
				ReasoningLevel:  candidate.DefaultReasoningLevel,
				ReasoningLevels: slices.Clone(candidate.ReasoningLevels),
				Thinking:        candidate.Thinking,
				APIFormat:       string(candidate.APIFormat),
				ContextWindow:   candidate.ContextWindow,
				MaxOutputTokens: candidate.MaxOutputTokens,
				BaseURL:         provider.BaseURL,
				ProxyURL:        provider.ProxyURL,
			}
			// 空等级定位默认选项，其他等级在加载时展开，查询不再覆盖默认值。
			key := modelKey{ProviderID: provider.ID, ModelID: candidate.ID}
			catalog.models[key] = prepared
			for _, level := range candidate.ReasoningLevels {
				key.ReasoningLevel = level
				prepared.ReasoningLevel = level
				catalog.models[key] = prepared
			}
			catalog.options = append(catalog.options, modelconfig.Option{
				GroupID:               candidate.GroupID,
				ProviderID:            provider.ID,
				ModelID:               candidate.ID,
				Label:                 candidate.DisplayName,
				ProviderName:          provider.DisplayName,
				ReasoningLevels:       slices.Clone(candidate.ReasoningLevels),
				DefaultReasoningLevel: candidate.DefaultReasoningLevel,
			})
		}
	}
	orderModelOptions(catalog.options, config.Groups)
	return catalog
}

func (c *Catalog) ListModels() []modelconfig.Option {
	options := slices.Clone(c.options)
	for index := range options {
		options[index].ReasoningLevels = slices.Clone(options[index].ReasoningLevels)
	}
	return options
}

func (c *Catalog) Get(providerID, modelID, reasoningLevel string) (Config, error) {
	config, ok := c.models[modelKey{
		ProviderID:     providerID,
		ModelID:        modelID,
		ReasoningLevel: reasoningLevel,
	}]
	if !ok {
		return Config{}, fmt.Errorf("model %q for provider %q with reasoning level %q is not configured", modelID, providerID, reasoningLevel)
	}
	return config, nil
}

func orderModelOptions(options []modelconfig.Option, groups []modelconfig.Group) {
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		groupOrder[group.ID] = index
	}
	slices.SortStableFunc(options, func(left, right modelconfig.Option) int {
		leftGroup, leftOK := groupOrder[left.GroupID]
		rightGroup, rightOK := groupOrder[right.GroupID]
		if leftOK && rightOK && leftGroup != rightGroup {
			return leftGroup - rightGroup
		}
		if leftOK != rightOK {
			if leftOK {
				return -1
			}
			return 1
		}
		if left.Label == right.Label {
			if left.ProviderID == right.ProviderID {
				return strings.Compare(left.ModelID, right.ModelID)
			}
			return strings.Compare(left.ProviderID, right.ProviderID)
		}
		return strings.Compare(left.Label, right.Label)
	})
}
