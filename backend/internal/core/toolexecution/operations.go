package toolexecution

import (
	"context"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// Operations 提供规则判断，不持有审批状态或保存工具结果。
// DecideRule 使用已准备操作的命令读取一次当前内存规则，后续授权与启动沿用返回值。
type Operations interface {
	DecideRule(context.Context, sessionentry.ToolCall, string) (security.ApprovalDecision, error)
}

// ControlRequest 将审批和取消绑定到消息声明的归属。
type ControlRequest struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
}
