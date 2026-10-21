package session

import (
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/fault"
)

// ModelChoice 表达用户选择的模型和推理等级；全空表示尚未选择或清除选择。
type ModelChoice struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// ModelSelection 保存一个会话已解析的模型配置，不属于持久化 Session 资料。
// 配置只通过副本进出，切换选择不会修改已冻结的执行配置。
type ModelSelection struct {
	config modelapi.Config
}

func NewModelSelection(config modelapi.Config) ModelSelection {
	return ModelSelection{config: config.Clone()}
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
		return modelapi.Config{}, fault.New(modelconfig.ModelNotConfigured, "")
	}
	return s.config.Clone(), nil
}
