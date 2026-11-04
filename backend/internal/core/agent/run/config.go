package run

import (
	"fmt"
	"regexp"

	modelapi "praxis/internal/core/model"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// RunConfig 是已冻结的执行配置。
type RunConfig struct {
	AgentDefinitionID identity.AgentDefinitionID
	SystemPrompt      string
	Model             modelapi.Config
}

// Clone 隔离模型配置中的可变能力列表。
func (s RunConfig) Clone() RunConfig {
	s.Model = s.Model.Clone()
	return s
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (s RunConfig) Validate() error {
	if !definitionIDPattern.MatchString(s.AgentDefinitionID.String()) {
		return fmt.Errorf("execution.config.agentDefinition: definition id is required: %w", errorcode.ErrInvalidValue)
	}
	if err := s.Model.Validate(); err != nil {
		return fmt.Errorf("execution.config.model: %w: %w", errorcode.ErrInvalidValue, err)
	}
	return nil
}

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
