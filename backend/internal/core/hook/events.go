package hook

import (
	"context"
	"fmt"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Error 标记扩展回调失败，业务层可与 Provider、工具及存储错误区分。
type Error struct {
	Point string
	Cause error
}

func (e *Error) Error() string { return fmt.Sprintf("%s hook: %v", e.Point, e.Cause) }
func (e *Error) Unwrap() error { return e.Cause }

// UserInput 尚未入队，不存在 Run；处理器只返回正文，不能更改目标 Session。
type UserInput struct {
	SessionID identity.SessionID
	Prompt    string
}

type BeforeUserInputHook interface {
	BeforeUserInput(context.Context, UserInput) (string, error)
}

// Iteration 对应一次正常模型响应及其工具调用，不包含摘要请求。
type Iteration struct {
	Identity
}

type IterationResult struct {
	Iteration
	StopReason providerapi.StopReason
	Err        error
}

type BeforeIterationHook interface {
	BeforeIteration(context.Context, Iteration) error
}

type AfterIterationHook interface {
	AfterIteration(context.Context, IterationResult) error
}

// ModelRequest 携带本次请求的只读快照，处理器不返回修改后的请求。
type ModelRequest struct {
	Identity
	Kind    sessionentry.ModelRequestKind
	Request providerapi.Request
}

// ModelResponse 在流消费结束后通知，包括断流和取消时已经收到的输出量。
type ModelResponse struct {
	Request     ModelRequest
	OutputBytes int
	StopReason  providerapi.StopReason
	Err         error
}

type BeforeModelRequestHook interface {
	BeforeModelRequest(context.Context, ModelRequest) error
}

type AfterModelResponseHook interface {
	AfterModelResponse(context.Context, ModelResponse) error
}

// ToolExecution 的调用归属只读；Before 只返回参数，不能替换工具或审批结果。
type ToolExecution struct {
	Identity
	Call      sessionentry.ToolCall
	Arguments string
}

type ToolExecutionResult struct {
	ToolExecution
	Result tool.ToolResult
	Err    error
}

type BeforeToolExecutionHook interface {
	BeforeToolExecution(context.Context, ToolExecution) error
}

// AfterToolExecutionHook 观察已提交的执行结果，不能覆盖回执；通知失败不改变执行事实。
type AfterToolExecutionHook interface {
	AfterToolExecution(context.Context, ToolExecutionResult) error
}

type ToolExecutionWaitingHook interface {
	ToolExecutionWaiting(context.Context, ToolExecution) error
}

// ContextBuilt 只读观察已构建的模型上下文，不允许修改共享 Entry 或返回替代正文。
type ContextBuilt struct {
	Identity
	Context appcontext.ModelContext
}

type AfterContextBuildHook interface {
	AfterContextBuild(context.Context, ContextBuilt) error
}

// ContextCompression 表示一次原生压缩策略调用；是否需要压缩由策略决定。
type ContextCompression struct {
	Identity
	Context appcontext.ModelContext
}

type ContextCompressionResult struct {
	ContextCompression
	ContextAfter appcontext.ModelContext
	Changed      bool
	Err          error
}

type BeforeContextCompressionHook interface {
	BeforeContextCompression(context.Context, ContextCompression) error
}

type AfterContextCompressionHook interface {
	AfterContextCompression(context.Context, ContextCompressionResult) error
}

// EntryCommitted 只携带首次保存成功的条目，不允许处理器改变已经提交的事实。
// 通知在业务锁外执行，不保证跨重启必达或并发提交的全局顺序。
type EntryCommitted struct {
	Identity
	Entry sessionentry.Entry
}

type UserInputCommittedHook interface {
	UserInputCommitted(context.Context, EntryCommitted) error
}

type AssistantCommittedHook interface {
	AssistantCommitted(context.Context, EntryCommitted) error
}

type FinalOutputCommittedHook interface {
	FinalOutputCommitted(context.Context, EntryCommitted) error
}

// RunEvent 只暴露已经确定的执行事实，不提供可变业务实体或进度写入能力。
type RunEvent struct {
	Identity
	Outcome     string
	FailureCode string
	// ResumeReason 区分进程恢复和审批唤醒；新执行为空。
	ResumeReason string
}

type RunStartedHook interface {
	RunStarted(context.Context, RunEvent) error
}

type RunResumedHook interface {
	RunResumed(context.Context, RunEvent) error
}

type RunCommittedHook interface {
	RunCommitted(context.Context, RunEvent) error
}
