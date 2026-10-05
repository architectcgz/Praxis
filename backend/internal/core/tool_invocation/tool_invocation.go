// Package toolinvocation 定义工具调用的持久化身份、状态转换与结算不变量。
// 状态修改不提供并发同步；调用方必须在事务内读取、转换并保存，终态不得重新执行。
package toolinvocation

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"praxis/internal/contracts"
)

// MaxInlineToolResultBytes 限制单次工具结果的内联字节数，执行器应在返回前完成截断。
const MaxInlineToolResultBytes = 50 * 1024

// ToolInvocationStatus 表示工具调用的持久化状态；unknown 是结果无法确认的终态。
type ToolInvocationStatus string

const (
	ToolInvocationRequested        ToolInvocationStatus = "requested"
	ToolInvocationAwaitingApproval ToolInvocationStatus = "awaiting_approval"
	ToolInvocationApproved         ToolInvocationStatus = "approved"
	ToolInvocationRunning          ToolInvocationStatus = "running"
	ToolInvocationSucceeded        ToolInvocationStatus = "succeeded"
	ToolInvocationFailed           ToolInvocationStatus = "failed"
	ToolInvocationDenied           ToolInvocationStatus = "denied"
	ToolInvocationInterrupted      ToolInvocationStatus = "interrupted"
	ToolInvocationUnknown          ToolInvocationStatus = "unknown"
)

// Valid 判断状态是否属于受支持的持久化状态集合。
func (s ToolInvocationStatus) Valid() bool {
	switch s {
	case ToolInvocationRequested, ToolInvocationAwaitingApproval, ToolInvocationApproved,
		ToolInvocationRunning, ToolInvocationSucceeded, ToolInvocationFailed,
		ToolInvocationDenied, ToolInvocationInterrupted, ToolInvocationUnknown:
		return true
	default:
		return false
	}
}

// Terminal 判断调用是否已结算；结果未知也不能自动重放，以免重复产生副作用。
func (s ToolInvocationStatus) Terminal() bool {
	switch s {
	case ToolInvocationSucceeded, ToolInvocationFailed, ToolInvocationDenied,
		ToolInvocationInterrupted, ToolInvocationUnknown:
		return true
	default:
		return false
	}
}

// ToolFailureCode 是工具调用持久化结果的稳定失败分类。
type ToolFailureCode string

const (
	ToolFailureNone            ToolFailureCode = ""
	ToolFailureNotAllowed      ToolFailureCode = "tool_not_allowed"
	ToolFailureApprovalDenied  ToolFailureCode = "tool_approval_denied"
	ToolFailureExecutionFailed ToolFailureCode = "tool_execution_failed"
	ToolFailureResultUnknown   ToolFailureCode = "tool_result_unknown"
	ToolFailureInterrupted     ToolFailureCode = "tool_interrupted"
)

// Valid 判断失败码是否受支持；空值表示没有失败。
func (c ToolFailureCode) Valid() bool {
	switch c {
	case ToolFailureNone, ToolFailureNotAllowed, ToolFailureApprovalDenied,
		ToolFailureExecutionFailed, ToolFailureResultUnknown, ToolFailureInterrupted:
		return true
	default:
		return false
	}
}

// ToolInvocationResult 保存有界的结果正文及错误、副作用和截断标记。
type ToolInvocationResult struct {
	InlineContent string
	ErrorCode     ToolFailureCode
	SideEffect    bool
	Truncated     bool
}

// Validate 只读校验结果大小和失败码，不截断正文或修正持久化数据。
func (r ToolInvocationResult) Validate() error {
	if len(r.InlineContent) > MaxInlineToolResultBytes {
		return contracts.InvalidValue("toolInvocation.result.inlineContent", "inline result exceeds the size limit")
	}
	if !r.ErrorCode.Valid() {
		return contracts.InvalidValue("toolInvocation.result.errorCode", "unknown tool failure code")
	}
	return nil
}

// ToolInvocation 归属于一次 Turn，以 ProviderToolCallID 和参数摘要识别重试。
// 仓储负责保证 (TurnID, ProviderToolCallID) 唯一，身份及规范化参数不可变。
type ToolInvocation struct {
	ID                  contracts.ToolInvocationID
	TurnID              contracts.TurnID
	SessionID           contracts.SessionID
	AgentID             contracts.AgentID
	ProviderToolCallID  string
	Name                contracts.ToolName
	NormalizedArguments json.RawMessage
	ArgumentsDigest     string
	Status              ToolInvocationStatus
	Approval            contracts.ApprovalRecord
	Result              ToolInvocationResult
	FailureCode         ToolFailureCode
	CreatedAt           time.Time
	ApprovedAt          time.Time
	StartedAt           time.Time
	SettledAt           time.Time
}

// NewToolInvocation 创建 requested 调用，复制参数并将创建时间转换为 UTC。
// 身份、Provider call ID、参数及摘要须由输入边界规范化；非法或空输入返回错误。
func NewToolInvocation(
	id contracts.ToolInvocationID,
	turnID contracts.TurnID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	providerToolCallID string,
	name contracts.ToolName,
	normalizedArguments json.RawMessage,
	argumentsDigest string,
	at time.Time,
) (ToolInvocation, error) {
	invocation := ToolInvocation{
		ID:                  id,
		TurnID:              turnID,
		SessionID:           sessionID,
		AgentID:             agentID,
		ProviderToolCallID:  providerToolCallID,
		Name:                name,
		NormalizedArguments: bytes.Clone(normalizedArguments),
		ArgumentsDigest:     argumentsDigest,
		Status:              ToolInvocationRequested,
		CreatedAt:           at.UTC(),
	}
	if err := invocation.Validate(); err != nil {
		return ToolInvocation{}, err
	}
	return invocation, nil
}

// Validate 只读校验身份、规范化输入、状态、结果和时间顺序；持久化恢复必须调用。
// 非 canonical 字符串和互相矛盾的生命周期字段会被拒绝，不会被静默修正。
func (i ToolInvocation) Validate() error {
	for _, value := range []string{
		string(i.ID),
		string(i.TurnID),
		string(i.SessionID),
		string(i.AgentID),
		i.ProviderToolCallID,
		i.ArgumentsDigest,
	} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return contracts.InvalidValue("toolInvocation", "identity and digest must be non-empty canonical values")
		}
	}
	if !i.Name.Valid() || len(i.NormalizedArguments) == 0 || !json.Valid(i.NormalizedArguments) {
		return contracts.InvalidValue("toolInvocation", "tool name and normalized JSON arguments are required")
	}
	if !i.Status.Valid() || !i.FailureCode.Valid() || i.CreatedAt.IsZero() {
		return contracts.InvalidValue("toolInvocation", "status, failure code, or creation time is invalid")
	}
	if err := i.Result.Validate(); err != nil {
		return err
	}
	if i.ApprovedAt.IsZero() {
		if i.Approval != (contracts.ApprovalRecord{}) || !i.StartedAt.IsZero() {
			return contracts.InvalidValue("toolInvocation.approval", "unapproved invocation has approval or start fields")
		}
	} else {
		if err := i.Approval.Validate(); err != nil {
			return contracts.FieldError("toolInvocation.approval", err)
		}
		if !i.Approval.ApprovedAt.Equal(i.ApprovedAt) ||
			i.Approval.PolicyFingerprint != strings.TrimSpace(i.Approval.PolicyFingerprint) {
			return contracts.InvalidValue("toolInvocation.approval", "approval time or fingerprint is inconsistent")
		}
	}
	previous := i.CreatedAt
	for _, at := range []time.Time{i.ApprovedAt, i.StartedAt, i.SettledAt} {
		if !at.IsZero() {
			if at.Before(previous) {
				return contracts.InvalidValue("toolInvocation.timestamps", "lifecycle timestamps are out of order")
			}
			previous = at
		}
	}
	if i.Status.Terminal() {
		if i.SettledAt.IsZero() || i.Result.ErrorCode != i.FailureCode {
			return contracts.InvalidValue("toolInvocation", "terminal invocation requires a settlement time and matching result code")
		}
	} else if !i.SettledAt.IsZero() || i.FailureCode != ToolFailureNone || i.Result != (ToolInvocationResult{}) {
		return contracts.InvalidValue("toolInvocation", "active invocation contains terminal fields")
	}
	if i.StartedAt.IsZero() && i.Result.SideEffect {
		return contracts.InvalidValue("toolInvocation.result", "unstarted invocation cannot report a side effect")
	}
	switch i.Status {
	case ToolInvocationRequested, ToolInvocationAwaitingApproval, ToolInvocationDenied:
		if !i.ApprovedAt.IsZero() {
			return contracts.InvalidValue("toolInvocation.approvedAt", "unapproved invocation has an approval time")
		}
	case ToolInvocationApproved:
		if i.ApprovedAt.IsZero() || !i.StartedAt.IsZero() {
			return contracts.InvalidValue("toolInvocation", "approved invocation has invalid lifecycle fields")
		}
	case ToolInvocationRunning, ToolInvocationSucceeded, ToolInvocationFailed, ToolInvocationUnknown:
		if i.StartedAt.IsZero() {
			return contracts.InvalidValue("toolInvocation.startedAt", "executed invocation requires a start time")
		}
	}
	switch i.Status {
	case ToolInvocationSucceeded:
		if i.FailureCode != ToolFailureNone {
			return contracts.InvalidValue("toolInvocation.result", "successful invocation cannot contain a failure")
		}
	case ToolInvocationFailed:
		if i.FailureCode == ToolFailureNone {
			return contracts.InvalidValue("toolInvocation.result", "failed invocation requires a failure code")
		}
	case ToolInvocationDenied:
		if i.FailureCode != ToolFailureNotAllowed && i.FailureCode != ToolFailureApprovalDenied {
			return contracts.InvalidValue("toolInvocation.result", "denial requires a denial failure code")
		}
	case ToolInvocationInterrupted:
		if i.FailureCode != ToolFailureInterrupted {
			return contracts.InvalidValue("toolInvocation.result", "interruption requires its stable failure code")
		}
	case ToolInvocationUnknown:
		if i.FailureCode != ToolFailureResultUnknown {
			return contracts.InvalidValue("toolInvocation.result", "unknown result requires its stable failure code")
		}
	}
	return nil
}

// Approve 接受 requested 或 awaiting_approval 调用的有效审批；失败不修改对象。
func (i *ToolInvocation) Approve(approval contracts.ApprovalRecord) error {
	if i.Status != ToolInvocationRequested && i.Status != ToolInvocationAwaitingApproval {
		return contracts.InvalidTransition("toolInvocation", string(i.Status), string(ToolInvocationApproved))
	}
	next := *i
	approval.ApprovedAt = approval.ApprovedAt.UTC()
	next.Status = ToolInvocationApproved
	next.Approval = approval
	next.ApprovedAt = approval.ApprovedAt
	return i.apply(next)
}

// Start 将 approved 调用转为 running；开始时间不得早于审批，失败不修改对象。
func (i *ToolInvocation) Start(at time.Time) error {
	if i.Status != ToolInvocationApproved {
		return contracts.InvalidTransition("toolInvocation", string(i.Status), string(ToolInvocationRunning))
	}
	next := *i
	next.Status = ToolInvocationRunning
	next.StartedAt = at.UTC()
	return i.apply(next)
}

// Succeed 结算 running 调用；结果不得带错误码，结算失败不修改对象。
func (i *ToolInvocation) Succeed(result ToolInvocationResult, at time.Time) error {
	return i.settle(ToolInvocationSucceeded, result, ToolFailureNone, at)
}

// Fail 结算 running 调用；code 必须非空且与结果匹配，失败不修改对象。
func (i *ToolInvocation) Fail(result ToolInvocationResult, code ToolFailureCode, at time.Time) error {
	return i.settle(ToolInvocationFailed, result, code, at)
}

// Deny 拒绝尚未审批的调用；结果只能使用权限或审批拒绝码，且不能产生副作用。
func (i *ToolInvocation) Deny(result ToolInvocationResult, at time.Time) error {
	return i.settle(ToolInvocationDenied, result, result.ErrorCode, at)
}

// Interrupt 中断任一非终态调用；允许恢复尚未启动的调用，错误码须为 interrupted。
func (i *ToolInvocation) Interrupt(result ToolInvocationResult, at time.Time) error {
	return i.settle(ToolInvocationInterrupted, result, ToolFailureInterrupted, at)
}

// MarkResultUnknown 结算结果丢失的 running 调用；保留已启动事实，禁止自动重放。
func (i *ToolInvocation) MarkResultUnknown(result ToolInvocationResult, at time.Time) error {
	return i.settle(ToolInvocationUnknown, result, ToolFailureResultUnknown, at)
}

func (i *ToolInvocation) settle(status ToolInvocationStatus, result ToolInvocationResult, code ToolFailureCode, at time.Time) error {
	allowed := false
	switch status {
	case ToolInvocationDenied:
		allowed = i.Status == ToolInvocationRequested || i.Status == ToolInvocationAwaitingApproval
	case ToolInvocationInterrupted:
		allowed = i.Status.Valid() && !i.Status.Terminal()
	default:
		allowed = i.Status == ToolInvocationRunning
	}
	if !allowed {
		return contracts.InvalidTransition("toolInvocation", string(i.Status), string(status))
	}
	next := *i
	next.Status = status
	next.Result = result
	next.FailureCode = code
	next.SettledAt = at.UTC()
	return i.apply(next)
}

// apply 先验证完整候选状态，避免时间、审批或结果校验失败后留下半更新对象。
func (i *ToolInvocation) apply(next ToolInvocation) error {
	if err := next.Validate(); err != nil {
		return err
	}
	*i = next
	return nil
}
