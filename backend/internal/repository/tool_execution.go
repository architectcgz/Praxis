package repository

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_execution"

	"context"
)

// ToolExecutionRepository 负责工具调用身份及结算结果的持久化。
type ToolExecutionRepository interface {
	Get(ctx context.Context, id contracts.ToolExecutionID) (toolmodel.Execution, error)

	FindByTaskCall(
		ctx context.Context,
		taskID contracts.TaskID,
		providerToolCallID string,
	) (toolmodel.Execution, error)

	// ListUnsettledBySession 返回 Session 全部非终态调用，供启动恢复一次读取；失败返回错误。
	ListUnsettledBySession(
		ctx context.Context,
		sessionID contracts.SessionID,
	) ([]toolmodel.Execution, error)

	ListByTask(context.Context, contracts.TaskID) ([]toolmodel.Execution, error)
	ListAwaitingByAgent(context.Context, contracts.AgentID) ([]toolmodel.Execution, error)
	Save(ctx context.Context, invocation toolmodel.Execution) error
}
