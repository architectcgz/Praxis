package agent

import (
	"praxis/internal/contracts"
)

const (
	DefinitionPrimary  contracts.AgentDefinitionID = "primary"
	DefinitionDelegate contracts.AgentDefinitionID = "delegate"
	DefinitionAdvisor  contracts.AgentDefinitionID = "advisor"
	DefinitionCurator  contracts.AgentDefinitionID = "curator"
)

// DisplayName 返回 Agent definition 对用户展示的英文名称；definition ID 只用于内部关联。
func DisplayName(id contracts.AgentDefinitionID) string {
	switch id {
	case DefinitionPrimary:
		return "Leader"
	case DefinitionDelegate:
		return "Builder"
	case DefinitionAdvisor:
		return "Scout"
	case DefinitionCurator:
		return "Editor"
	default:
		return "Agent"
	}
}

// AgentDefinition 描述可跨 Session 复用的 Agent 身份与默认指令。
type AgentDefinition struct {
	ID           contracts.AgentDefinitionID
	Profile      contracts.AgentProfile
	Instructions string
	Revision     string
}

// Snapshot 返回定义副本。
func (d AgentDefinition) Snapshot() AgentDefinition {
	return d
}
