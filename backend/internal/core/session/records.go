package session

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// RunRecords 提供会话持有的执行事实和模型请求记录，不创建运行实例。
type RunRecords interface {
	GetRun(context.Context, identity.SessionID, identity.RunID) (agentrun.RunRecord, error)
	SaveRun(context.Context, agentrun.RunRecord) (*sessionentry.ContextEntry, error)
	SaveModelRequest(context.Context, agentrun.RunRecord, agentrun.ModelRequestRecord) error
	CreateSubagent(context.Context, identity.BranchID, agentrun.RunRecord, sessionentry.ContextEntry) (*sessionentry.ContextEntry, error)
	FindRunByTask(context.Context, identity.AgentID, identity.TaskID) (agentrun.RunRecord, error)
}
