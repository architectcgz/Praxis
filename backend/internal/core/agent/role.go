package agent

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
