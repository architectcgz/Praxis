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

// AgentDefinition 描述可跨 Session 复用的角色定义；名称仅用于展示，角色与指令来自 ROLE.md。
type AgentDefinition struct {
	ID           identity.AgentDefinitionID
	Name         string
	Role         AgentRole
	Instructions string
}

// Snapshot 返回定义副本。
func (d AgentDefinition) Snapshot() AgentDefinition {
	return d
}
