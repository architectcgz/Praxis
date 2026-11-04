package toolexecution

import "praxis/internal/identity"

// ControlRequest 将审批和取消绑定到消息声明的归属。
type ControlRequest struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RunID     identity.RunID
	CallID    string
}
