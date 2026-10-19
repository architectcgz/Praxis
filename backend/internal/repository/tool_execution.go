package repository

import (
	"context"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

// ToolExecution 保存从工具请求登记到结果结算的执行事实。
type ToolExecution interface {
	Get(context.Context, identity.ToolExecutionID) (toolexecution.Execution, error)
	FindByExecutionCall(context.Context, identity.AgentExecutionID, string) (toolexecution.Execution, error)
	ListUnsettledBySession(context.Context, identity.SessionID) ([]toolexecution.Execution, error)
	ListByExecution(context.Context, identity.AgentExecutionID) ([]toolexecution.Execution, error)
	ListAwaitingByAgent(context.Context, identity.AgentID) ([]toolexecution.Execution, error)
	Save(context.Context, toolexecution.Execution) error
}
