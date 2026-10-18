// Package toolcall 定义工具调用的持久化身份、状态转换与结算不变量。
// 状态修改不提供并发同步；调用方必须在事务内读取、转换并保存，终态不得重新执行。
package toolcall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/security"
	"praxis/internal/tool"
	"time"
)

// Status 表示工具调用的持久化状态；unknown 是结果无法确认的终态。
type Status string

const (
	StatusRequested        Status = "requested"
	StatusPrepared         Status = "prepared"
	StatusReviewing        Status = "reviewing"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusApproved         Status = "approved"
	StatusRunning          Status = "running"
	StatusSucceeded        Status = "succeeded"
	StatusFailed           Status = "failed"
	StatusDenied           Status = "denied"
	StatusInterrupted      Status = "interrupted"
	StatusUnknown          Status = "unknown"
)

// Valid 判断状态是否属于受支持的持久化状态集合。
func (s Status) Valid() bool {
	switch s {
	case StatusRequested, StatusPrepared, StatusReviewing, StatusAwaitingApproval, StatusApproved,
		StatusRunning, StatusSucceeded, StatusFailed,
		StatusDenied, StatusInterrupted, StatusUnknown:
		return true
	default:
		return false
	}
}

// Terminal 判断调用是否已结算；结果未知也不能自动重放，以免重复产生副作用。
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusDenied,
		StatusInterrupted, StatusUnknown:
		return true
	default:
		return false
	}
}

// FailureCode 是工具调用持久化结果的稳定失败分类。
type FailureCode string

const (
	FailureNone            FailureCode = ""
	FailureResourceLimit   FailureCode = "tool_call_limit"
	FailureInvalidCall     FailureCode = "invalid_tool_call"
	FailureNotAllowed      FailureCode = "tool_not_allowed"
	FailureApprovalDenied  FailureCode = "tool_approval_denied"
	FailureRiskDenied      FailureCode = "tool_risk_denied"
	FailureExecutionFailed FailureCode = "tool_execution_failed"
	FailureResultUnknown   FailureCode = "tool_result_unknown"
	FailureInterrupted     FailureCode = "tool_interrupted"
)

// Valid 判断失败码是否受支持；空值表示没有失败。
func (c FailureCode) Valid() bool {
	switch c {
	case FailureResourceLimit, FailureNone, FailureInvalidCall, FailureNotAllowed, FailureApprovalDenied, FailureRiskDenied,
		FailureExecutionFailed, FailureResultUnknown, FailureInterrupted:
		return true
	default:
		return false
	}
}

// Result 保存有界的结果正文及错误、副作用和截断标记。
type Result struct {
	InlineContent string
	ErrorCode     FailureCode
	SideEffect    bool
	Truncated     bool
}

// Execution 归属于一次 Task 的一次 loop 迭代（Turn），以 ProviderToolCallID
// 和参数摘要识别重试。仓储负责保证 (ExecutionID, ProviderToolCallID) 唯一，
// 原始调用身份不可变；规范化参数在准入时冻结，之后不可变。
type Execution struct {
	ID                  identity.ToolExecutionID
	ExecutionID         identity.AgentExecutionID
	TurnID              identity.TurnID
	SessionID           identity.SessionID
	AgentID             identity.AgentID
	ProviderToolCallID  string
	Name                tool.ToolName
	Arguments           json.RawMessage `json:",omitempty"`
	NormalizedArguments json.RawMessage `json:",omitempty"`
	Path                string          `json:",omitempty"`
	ArgumentsDigest     string
	Status              Status
	BudgetReserved      bool
	Decision            security.ApprovalDecision
	Reviews             []RiskReview `json:",omitempty"`
	Approval            ApprovalRecord
	Result              Result
	FailureCode         FailureCode
	CreatedAt           time.Time
	ApprovedAt          time.Time
	StartedAt           time.Time
	SettledAt           time.Time
}

// NewExecution 创建 requested 调用，复制参数并将创建时间转换为 UTC。
// 身份和原始 JSON 已由输入边界校验，具体工具参数在执行准入时处理。
func NewExecution(
	id identity.ToolExecutionID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	providerToolCallID string,
	name tool.ToolName,
	arguments json.RawMessage,
	argumentsDigest string,
	at time.Time,
) Execution {
	return Execution{
		ID:                 id,
		ExecutionID:        executionID,
		TurnID:             turnID,
		SessionID:          sessionID,
		AgentID:            agentID,
		ProviderToolCallID: providerToolCallID,
		Name:               name,
		Arguments:          bytes.Clone(arguments),
		ArgumentsDigest:    argumentsDigest,
		Status:             StatusRequested,
		CreatedAt:          at.UTC(),
	}
}

// validateState 检查本次状态变更所需的生命周期和授权关系，不重查身份或参数格式。
func (i Execution) validateState() error {
	if err := i.validateAuthorization(); err != nil {
		return err
	}
	if !i.ApprovedAt.IsZero() && len(i.NormalizedArguments) == 0 {
		return fault.InvalidValue("toolExecution", "approved invocation requires normalized arguments")
	}
	if !i.Status.Valid() || !i.FailureCode.Valid() || i.CreatedAt.IsZero() {
		return fault.InvalidValue("toolExecution", "status, failure code, or creation time is invalid")
	}
	if !i.Result.ErrorCode.Valid() {
		return fault.InvalidValue("toolExecution.result.errorCode", "unknown tool failure code")
	}
	if i.ApprovedAt.IsZero() {
		if i.Approval != (ApprovalRecord{}) || !i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.approval", "unapproved invocation has approval or start fields")
		}
	} else {
		switch i.Approval.Source {
		case ApprovalSourceUser, ApprovalSourceYolo, ApprovalSourceAuto:
		default:
			return fault.InvalidValue("toolExecution.approval.source", "unknown approval source")
		}
		if !i.Approval.ApprovedAt.Equal(i.ApprovedAt) ||
			i.Approval.ArgumentsDigest != ArgumentsDigest(i.Name, i.NormalizedArguments) {
			return fault.InvalidValue("toolExecution.approval", "approval time or arguments digest is inconsistent")
		}
	}
	previous := i.CreatedAt
	for _, at := range []time.Time{i.ApprovedAt, i.StartedAt, i.SettledAt} {
		if !at.IsZero() {
			if at.Before(previous) {
				return fault.InvalidValue("toolExecution.timestamps", "lifecycle timestamps are out of order")
			}
			previous = at
		}
	}
	if i.Status.Terminal() {
		if i.SettledAt.IsZero() || i.Result.ErrorCode != i.FailureCode {
			return fault.InvalidValue("toolExecution", "terminal invocation requires a settlement time and matching result code")
		}
	} else if !i.SettledAt.IsZero() || i.FailureCode != FailureNone || i.Result != (Result{}) {
		return fault.InvalidValue("toolExecution", "active invocation contains terminal fields")
	}
	if i.StartedAt.IsZero() && i.Result.SideEffect {
		return fault.InvalidValue("toolExecution.result", "unstarted invocation cannot report a side effect")
	}
	switch i.Status {
	case StatusRequested, StatusPrepared, StatusReviewing, StatusAwaitingApproval, StatusDenied:
		if !i.ApprovedAt.IsZero() {
			return fault.InvalidValue("toolExecution.approvedAt", "unapproved invocation has an approval time")
		}
	case StatusApproved:
		if i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution", "approved invocation has invalid lifecycle fields")
		}
	case StatusRunning, StatusSucceeded, StatusFailed, StatusUnknown:
		if i.StartedAt.IsZero() {
			return fault.InvalidValue("toolExecution.startedAt", "executed invocation requires a start time")
		}
	}
	switch i.Status {
	case StatusSucceeded:
		if i.FailureCode != FailureNone {
			return fault.InvalidValue("toolExecution.result", "successful invocation cannot contain a failure")
		}
	case StatusFailed:
		if i.FailureCode == FailureNone {
			return fault.InvalidValue("toolExecution.result", "failed invocation requires a failure code")
		}
	case StatusDenied:
		if i.FailureCode != FailureResourceLimit && i.FailureCode != FailureNotAllowed && i.FailureCode != FailureApprovalDenied && i.FailureCode != FailureRiskDenied && i.FailureCode != FailureInvalidCall {
			return fault.InvalidValue("toolExecution.result", "denial requires a denial failure code")
		}
	case StatusInterrupted:
		if i.FailureCode != FailureInterrupted {
			return fault.InvalidValue("toolExecution.result", "interruption requires its stable failure code")
		}
	case StatusUnknown:
		if i.FailureCode != FailureResultUnknown {
			return fault.InvalidValue("toolExecution.result", "unknown result requires its stable failure code")
		}
	}
	return nil
}

// AwaitApproval 保留冻结参数并等待用户批准；尚未启动的旧授权被撤销时清空审批字段。
func (i *Execution) AwaitApproval() error {
	if i.Status == StatusAwaitingApproval || !canTransition(i.Status, StatusAwaitingApproval) {
		return fault.InvalidTransition("toolExecution", string(i.Status), string(StatusAwaitingApproval))
	}
	next := *i
	next.Status = StatusAwaitingApproval
	next.Approval = ApprovalRecord{}
	next.ApprovedAt = time.Time{}
	return i.apply(next)
}

// Approve 接受 requested 或 awaiting_approval 调用的有效审批；失败不修改对象。
func (i *Execution) Approve(approval ApprovalRecord) error {
	if i.Status == StatusApproved || !canTransition(i.Status, StatusApproved) {
		return fault.InvalidTransition("toolExecution", string(i.Status), string(StatusApproved))
	}
	next := *i
	approval.ApprovedAt = approval.ApprovedAt.UTC()
	next.Status = StatusApproved
	next.Approval = approval
	next.ApprovedAt = approval.ApprovedAt
	return i.apply(next)
}

// Start 将 approved 调用转为 running；开始时间不得早于审批，失败不修改对象。
func (i *Execution) Start(at time.Time) error {
	if i.Status == StatusRunning || !canTransition(i.Status, StatusRunning) {
		return fault.InvalidTransition("toolExecution", string(i.Status), string(StatusRunning))
	}
	next := *i
	next.Status = StatusRunning
	next.StartedAt = at.UTC()
	return i.apply(next)
}

// Succeed 结算 running 调用；结果不得带错误码，结算失败不修改对象。
func (i *Execution) Succeed(result Result, at time.Time) error {
	return i.settle(StatusSucceeded, result, FailureNone, at)
}

// Fail 结算 running 调用；code 必须非空且与结果匹配，失败不修改对象。
func (i *Execution) Fail(result Result, code FailureCode, at time.Time) error {
	return i.settle(StatusFailed, result, code, at)
}

// Deny 拒绝尚未审批的调用；参数无效或权限拒绝均不能产生副作用。
func (i *Execution) Deny(result Result, at time.Time) error {
	return i.settle(StatusDenied, result, result.ErrorCode, at)
}

// Interrupt 中断任一非终态调用；允许恢复尚未启动的调用，错误码须为 interrupted。
func (i *Execution) Interrupt(result Result, at time.Time) error {
	return i.settle(StatusInterrupted, result, FailureInterrupted, at)
}

// MarkResultUnknown 结算结果丢失的 running 调用；保留已启动事实，禁止自动重放。
func (i *Execution) MarkResultUnknown(result Result, at time.Time) error {
	return i.settle(StatusUnknown, result, FailureResultUnknown, at)
}

func (i *Execution) settle(status Status, result Result, code FailureCode, at time.Time) error {
	allowed := i.Status != status && canTransition(i.Status, status)
	if !allowed {
		return fault.InvalidTransition("toolExecution", string(i.Status), string(status))
	}
	next := *i
	next.Status = status
	next.Result = result
	next.FailureCode = code
	next.SettledAt = at.UTC()
	return i.apply(next)
}

// apply 先检查状态转换和本次产生的结果，失败时保留原对象；输入格式由入口负责。
func (i *Execution) apply(next Execution) error {
	if next.Result != i.Result && len(next.Result.InlineContent) > tool.MaxInlineResultBytes {
		return fault.InvalidValue("toolExecution.result.inlineContent", "inline result exceeds the size limit")
	}
	if err := next.validateState(); err != nil {
		return err
	}
	if err := ValidateUpdate(*i, next); err != nil {
		return err
	}
	*i = next
	return nil
}

// ArgumentsDigest 固定原始请求身份；不同归一化顺序不改变调用意图。
func ArgumentsDigest(name tool.ToolName, arguments json.RawMessage) string {
	encoded, err := json.Marshal(struct {
		Name      tool.ToolName
		Arguments json.RawMessage
	}{Name: name, Arguments: arguments})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
