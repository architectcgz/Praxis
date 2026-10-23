package repository

import (
	"context"
	"praxis/internal/core/toolexecution"
)

// ToolExecution 只保存可靠审批事实，工具完成必须查询消息回执。
type ToolExecution interface {
	GetApprovalRequest(context.Context, toolexecution.Key) (toolexecution.ApprovalRequest, bool, error)
	GetUserDecision(context.Context, toolexecution.Key) (toolexecution.UserDecision, bool, error)
	GetReview(context.Context, toolexecution.Key) (toolexecution.ReviewRecord, bool, error)
	SaveAuthorization(context.Context, toolexecution.Event) error
}
