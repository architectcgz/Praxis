package agent

import (
	"fmt"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
)

// ValidateDefinitions 要求默认定义存在，并禁止移除已有定义；角色属性允许重载。
// 首次加载时 current 为空，只检查候选定义的业务约束。
func ValidateDefinitions(current, candidate []agentmodel.AgentDefinition) error {
	definitions := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(candidate))
	for _, definition := range candidate {
		definitions[definition.ID] = definition
	}
	_, ok := definitions[agentmodel.DefinitionPrimary]
	if !ok {
		return fmt.Errorf("agent definition %q is required", agentmodel.DefinitionPrimary)
	}
	for _, previous := range current {
		_, ok := definitions[previous.ID]
		if !ok {
			return fmt.Errorf("loaded agent definition %q cannot be removed", previous.ID)
		}
	}
	return nil
}
