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
	SaveRun(context.Context, agentrun.RunRecord) error
	SaveModelRequest(context.Context, agentrun.RunRecord, agentrun.ModelRequestRecord) error
	CreateSubagent(context.Context, agentrun.RunRecord, sessionentry.ContextEntry) (*sessionentry.ContextEntry, error)
	FindRunByRequest(context.Context, identity.AgentID, identity.RequestID) (agentrun.RunRecord, error)
	ListAgentRuns(context.Context, identity.SessionID, identity.AgentID, identity.BranchID, int) ([]agentrun.RunRecord, error)
	ListSessionRuns(context.Context, identity.SessionID, identity.BranchID) ([]agentrun.RunRecord, error)
}
