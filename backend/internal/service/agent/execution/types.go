package execution

import (
	"context"
	"praxis/internal/identity"
)

// ActiveTasks 为 Execution 提供进程内执行身份，具体生命周期由 Coordinator 管理。
type ActiveTasks interface {
	Reserve(context.Context, identity.AgentID, identity.AgentExecutionID) (context.Context, bool, error)
	Release(identity.AgentID, identity.AgentExecutionID)
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}
