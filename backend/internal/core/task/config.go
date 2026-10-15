package task

import (
	"praxis/internal/contracts"
	"praxis/internal/core/model"
	"praxis/internal/utils/pathutil"
)

// Config 是已冻结的执行配置。
type Config struct {
	AgentDefinitionID contracts.AgentDefinitionID
	SystemPrompt      string
	Model             model.Config
	WorkspacePath     string
	ResourceLimits    contracts.ResourceLimits
}

func (s Config) Validate() error {
	if !s.AgentDefinitionID.Valid() {
		return contracts.InvalidValue("task.config.agentDefinition", "definition id is required")
	}

	if err := s.Model.Validate(); err != nil {
		return contracts.FieldError("task.config.model", err)
	}
	if !pathutil.IsAbsoluteNormalized(s.WorkspacePath) {
		return contracts.InvalidValue("task.config.workspacePath", "workspace path must be an absolute normalized path")
	}
	if err := s.ResourceLimits.Validate(); err != nil {
		return contracts.FieldError("task.config.resourceLimits", err)
	}
	return nil
}

// Clone 隔离模型配置中的可变能力列表。
func (s Config) Clone() Config { s.Model = s.Model.Clone(); return s }
