package toolexecution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Operations 提供规则判断和结果提交，不持有审批状态。
// DecideRule 使用已准备操作的命令读取一次当前内存规则，后续授权与启动沿用返回值。
type Operations interface {
	DecideRule(context.Context, sessionentry.ToolCall, string) (security.ApprovalDecision, error)
	CommitResult(context.Context, *agentexecution.Run, sessionentry.ToolCall, tool.ToolResult) (sessionentry.ContextEntry, error)
}

// ControlRequest 将审批和取消绑定到消息声明的归属。
type ControlRequest struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
}
