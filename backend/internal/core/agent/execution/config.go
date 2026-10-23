package execution

import (
	modelapi "praxis/internal/core/model"
	"praxis/internal/identity"
)

// ExecutionConfig 是已冻结的执行配置。
type ExecutionConfig struct {
	AgentDefinitionID identity.AgentDefinitionID
	SystemPrompt      string
	Model             modelapi.Config
}

// Clone 隔离模型配置中的可变能力列表。
func (s ExecutionConfig) Clone() ExecutionConfig {
	s.Model = s.Model.Clone()
	return s
}
