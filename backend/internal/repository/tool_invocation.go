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

	ListUnsettledByTask(
		ctx context.Context,
		taskID contracts.TaskID,
	) ([]toolmodel.ToolInvocation, error)

	Save(ctx context.Context, invocation toolmodel.ToolInvocation) error
}
