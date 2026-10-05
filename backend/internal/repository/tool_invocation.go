package repository

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_invocation"

	"context"
)

// ToolInvocationRepository 负责工具调用身份及结算结果的持久化。
type ToolInvocationRepository interface {
	Get(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error)

	FindByTurnCall(
		ctx context.Context,
		turnID contracts.TurnID,
		providerToolCallID string,
	) (toolmodel.ToolInvocation, error)

	ListUnsettledByTurn(
		ctx context.Context,
		turnID contracts.TurnID,
	) ([]toolmodel.ToolInvocation, error)

	Save(ctx context.Context, invocation toolmodel.ToolInvocation) error
}
