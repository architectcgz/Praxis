package agent

import (
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type AgentState string

const (
	AgentIdle        AgentState = "idle"
	AgentExecuting   AgentState = "executing"
	AgentWaiting     AgentState = "waiting"
	AgentInterrupted AgentState = "interrupted"
	AgentFailed      AgentState = "failed"
)

// AgentRole 表示角色类型，不作为 Agent 的名称或实例标识。
type AgentRole string

const (
	RolePrimary  AgentRole = "primary"
	RoleDelegate AgentRole = "delegate"
	RoleAdvisor  AgentRole = "advisor"
	RoleCurator  AgentRole = "curator"
)

// Valid 判断角色类型是否受支持。
func (r AgentRole) Valid() bool {
	switch r {
	case RolePrimary, RoleDelegate, RoleAdvisor, RoleCurator:
		return true
	default:
		return false
	}
}

const (
	DefinitionPrimary  identity.AgentDefinitionID = "primary"
	DefinitionDelegate identity.AgentDefinitionID = "delegate"
)

// AgentDefinition 描述可跨 Session 复用的角色定义；名称仅用于展示，角色与指令来自 AGENT.md。
type AgentDefinition struct {
	ID           identity.AgentDefinitionID
	Name         string
	Role         AgentRole
	Instructions string
}

const ErrExecuting errorcode.Code = "agent.executing"
