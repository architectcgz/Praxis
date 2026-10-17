package task

import (
	"praxis/internal/contracts"
	"praxis/internal/core/model"
)

// Config 是已冻结的执行配置。
type Config struct {
	AgentDefinitionID contracts.AgentDefinitionID
	SystemPrompt      string
	Model             model.Config
	WorkspacePath     string
}

// Clone 隔离模型配置中的可变能力列表。
func (s Config) Clone() Config {
	s.Model = s.Model.Clone()
	return s
}
