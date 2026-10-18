package repository

import (
	toolmodel "praxis/internal/core/tool_execution"

	"context"
	"praxis/internal/core/identity"
)

// ToolExecutionRepository 负责工具调用身份及结算结果的持久化。
type ToolExecutionRepository interface {
	Get(ctx context.Context, id identity.ToolExecutionID) (toolmodel.Execution, error)

	FindByTaskCall(
		ctx context.Context,
		taskID identity.TaskID,
		providerToolCallID string,
	) (toolmodel.Execution, error)

	// ListUnsettledBySession 返回 Session 全部非终态调用，供启动恢复一次读取；失败返回错误。
	ListUnsettledBySession(
		ctx context.Context,
		sessionID identity.SessionID,
	) ([]toolmodel.Execution, error)

	ListByTask(context.Context, identity.TaskID) ([]toolmodel.Execution, error)
	ListAwaitingByAgent(context.Context, identity.AgentID) ([]toolmodel.Execution, error)
	Save(ctx context.Context, invocation toolmodel.Execution) error
}
