package repository

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/tool_invocation"

	"context"
)

// ToolInvocationRepository 负责工具调用身份及结算结果的持久化。
type ToolInvocationRepository interface {
	Get(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error)

	FindByExecutionCall(
		ctx context.Context,
		executionID contracts.AgentExecutionID,
		providerToolCallID string,
	) (toolmodel.ToolInvocation, error)

	Save(ctx context.Context, invocation toolmodel.ToolInvocation) error
}
