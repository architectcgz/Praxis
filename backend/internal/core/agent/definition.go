package agent

import (
	"praxis/internal/identity"
)

const (
	DefinitionPrimary  identity.AgentDefinitionID = "primary"
	DefinitionDelegate identity.AgentDefinitionID = "delegate"
	DefinitionAdvisor  identity.AgentDefinitionID = "advisor"
	DefinitionCurator  identity.AgentDefinitionID = "curator"
)

// AgentDefinition 描述可跨 Session 复用的 Agent 身份与默认指令。
type AgentDefinition struct {
	ID           identity.AgentDefinitionID
	Profile      AgentProfile
	Instructions string
}

// Snapshot 返回定义副本。
func (d AgentDefinition) Snapshot() AgentDefinition {
	return d
}
