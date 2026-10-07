package repository

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_invocation"

	"context"
)

// ToolInvocationRepository 负责工具调用身份及结算结果的持久化。
type ToolInvocationRepository interface {
	Get(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error)

	FindByTaskCall(
		ctx context.Context,
		taskID contracts.TaskID,
		providerToolCallID string,
	) (toolmodel.ToolInvocation, error)

	// ListUnsettledBySession 返回 Session 全部非终态调用，供启动恢复一次读取；失败返回错误。
	ListUnsettledBySession(
		ctx context.Context,
		sessionID contracts.SessionID,
	) ([]toolmodel.ToolInvocation, error)

	Save(ctx context.Context, invocation toolmodel.ToolInvocation) error
}
