package task

import (
	"praxis/internal/identity"
	modelapi "praxis/internal/model"
)

// Config 是已冻结的执行配置。
type Config struct {
	AgentDefinitionID identity.AgentDefinitionID
	SystemPrompt      string
	Model             modelapi.Config
	WorkspacePath     string
}

// Clone 隔离模型配置中的可变能力列表。
func (s Config) Clone() Config {
	s.Model = s.Model.Clone()
	return s
}
