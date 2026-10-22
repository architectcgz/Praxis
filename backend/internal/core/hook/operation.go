// Package hook 定义业务执行边界的扩展协议，提供强类型数据扩展、观察通知和操作计时。
package hook

import (
	"context"
	"crypto/rand"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

// Identity 显式关联执行归属；OperationID 标识本次操作尝试，恢复后重新生成。
// 提交通知没有操作尝试，OperationID 为空；没有迭代边界时 TurnID 为空。
type Identity struct {
	OperationID  string
	SourceTaskID identity.TaskID
	SessionID    identity.SessionID
	AgentID      identity.AgentID
	ExecutionID  identity.AgentExecutionID
	TurnID       identity.TurnID
}

// NewOperationID 为一次操作尝试生成关联标识，Before、After 和 Scope 共用该值。
func NewOperationID() string { return rand.Text() }

type OperationKind string

const (
	OperationAgent           OperationKind = "agent"
	OperationProvider        OperationKind = "provider"
	OperationTool            OperationKind = "tool"
	OperationSessionCreate   OperationKind = "session_create"
	OperationExecutionSettle OperationKind = "execution_settle"
)

// Operation 只携带标识与名称，不包含 Prompt、工具参数、响应正文或凭据。
type Operation struct {
	Identity
	Kind        OperationKind
	Name        string
	ReferenceID string
}

// Result 是执行边界已确定的结果，Hook 不得修改业务结果。
type Result struct {
	Err            error
	Outcome        string
	StopReason     providerapi.StopReason
	ToolErrorClass string
}

type Signal string

const SignalFirstResponse Signal = "first_response"

// Scope 保存单次操作的独立状态；调用方只结束一次生命周期。
type Scope interface {
	// OnSignal 接收运行信号；同一信号可能多次出现，由具体 Scope 处理去重。
	OnSignal(Signal)
	// End 观察操作结果并释放资源，不改变执行结果。
	End(Result)
}

// OperationHook 扩展有明确开始与结束边界的操作，Start/End 对应执行前后。
// 进入执行边界的每次调用都有独立 Scope，包括幂等重试；结束结果只用于观察。
// 回调在业务事务和互斥锁之外执行，不承担授权、事务仲裁或取消业务执行的职责。
type OperationHook interface {
	// Start 必须返回非 nil Context；可返回 nil Scope 表示忽略该操作。
	Start(context.Context, Operation) (context.Context, Scope)
}
