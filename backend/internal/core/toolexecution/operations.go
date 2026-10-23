package toolexecution

import (
	"context"
	"encoding/json"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Operations 提供归属检查、配置读取、审核输入准备和可靠事实提交，不承接工具流程。
// DecideRule 为每次 ToolCall 读取一次当前内存规则，后续授权与启动沿用返回值。
type Operations interface {
	ResolveCall(context.Context, ControlRequest) (sessionentry.ToolCall, agentexecution.Execution, error)
	DecideRule(context.Context, sessionentry.ToolCall, json.RawMessage) (security.ApprovalDecision, error)
	PrepareReview(context.Context, sessionentry.ToolCall, string) (ReviewRequest, error)
	GetApprovalRequest(context.Context, Key) (ApprovalRequest, bool, error)
	GetUserDecision(context.Context, Key) (UserDecision, bool, error)
	GetReview(context.Context, Key) (ReviewRecord, bool, error)
	SaveAuthorization(context.Context, Event) error
	GetResult(context.Context, Key) (sessionentry.ContextEntry, bool, error)
	CommitResult(context.Context, sessionentry.ToolCall, tool.ToolResult) (sessionentry.ContextEntry, error)
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
	Call          sessionentry.ToolCall
	Arguments     json.RawMessage
	Decision      security.ApprovalDecision
	Model         modelapi.Config
	UserTask      string
	WorkspacePath string
	Attempt       RiskReview
}

type Reviewer struct {
	Provider       providerapi.Provider
	TimingRecorder *timing.Recorder
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
}
