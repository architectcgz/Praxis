package session

import (
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
)

// ModelChoice 表达用户选择的模型和推理等级；查询结果全空表示尚未选择。
type ModelChoice struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// ModelSelection 保存一个会话已解析的模型配置，不属于持久化 Session 资料。
// 选择与执行共享只读模型配置，切换时替换选择，不修改已发布配置。
type ModelSelection struct {
	config modelapi.Config
}

func NewModelSelection(config modelapi.Config) ModelSelection {
	return ModelSelection{config: config}
}

func (s ModelSelection) Choice() ModelChoice {
	return ModelChoice{
		ProviderID:     s.config.ProviderID,
		ModelID:        s.config.ModelID,
		ReasoningLevel: s.config.ReasoningLevel,
	}
}

func (s ModelSelection) CurrentModelConfig() (modelapi.Config, error) {
	if s.config.ProviderID == "" || s.config.ModelID == "" {
		return modelapi.Config{}, modelconfig.ErrNotConfigured
	}
	return s.config, nil
}
