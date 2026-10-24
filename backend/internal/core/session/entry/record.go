package entry

import "praxis/internal/identity"

// Record 显式携带日志所属 Session 和消息所属 Agent，存储不依赖内存实例查询。
type Record struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	Data      ContextEntry
}
