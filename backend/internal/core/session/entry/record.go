package entry

import "praxis/internal/identity"

// Record 以 AgentID 唯一确定消息归属；Session 由所属 Agent 决定。
type Record struct {
	AgentID identity.AgentID
	Data    ContextEntry
}
