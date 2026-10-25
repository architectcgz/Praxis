package toolexecution

import (
	"context"
	"encoding/json"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Operations 提供归属检查、配置读取、审核输入准备和结果提交，不持有审批状态。
// DecideRule 为每次 ToolCall 读取一次当前内存规则，后续授权与启动沿用返回值。
type Operations interface {
	DecideRule(context.Context, sessionentry.ToolCall, json.RawMessage) (security.ApprovalDecision, error)
	PrepareReview(context.Context, *agentexecution.Run, sessionentry.ToolCall, string) (ReviewRequest, error)
	CommitResult(context.Context, *agentexecution.Run, sessionentry.ToolCall, tool.ToolResult) (sessionentry.ContextEntry, error)
}

// ControlRequest 将审批和取消绑定到消息声明的归属。
type ControlRequest struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
}

// ReviewRequest 由业务服务预留模型请求预算，Core 调用 Provider 审核。
type ReviewRequest struct {
	Run           *agentexecution.Run
	Call          sessionentry.ToolCall
	Arguments     json.RawMessage
	Decision      security.ApprovalDecision
	Model         modelapi.Config
	UserTask      string
	WorkspacePath string
	Attempt       RiskReview
}

type Reviewer struct {
	Progress interface {
		RecordModelOutput(context.Context, *agentexecution.Run, int) error
	}
	Provider       providerapi.Provider
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
}
