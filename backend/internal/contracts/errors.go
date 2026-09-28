package contracts

import (
	"errors"
	"fmt"
	"strings"
)

// Code 是业务错误码。
type Code string

// Error 是业务层统一的错误载体。
type Error struct {
	Code    Code
	Message string
	Cause   error
}

// New 构造一个业务错误。
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap 构造一个带 cause 的业务错误，cause 不参与对外文案。
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// Error 实现 error；有 Message 时优先返回 Message，否则退化为业务码。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

// Unwrap 暴露 cause，供内部 errors.Is/As 判断。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorCode 返回稳定业务码，供边界（app 层）序列化透传。
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

// Is 允许按业务码匹配，使包装后的错误仍可用 errors.Is 判断。
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other != nil && e != nil && other.Code == e.Code
}

// CodeOf 从错误链中提取最外层业务码。
func CodeOf(err error) (Code, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}

// 通用/校验域：与具体业务对象无关的请求与基础设施类错误。
const (
	InvalidValueCode      Code = "generic.invalid_value"
	InvalidTransitionCode Code = "generic.invalid_transition"
	InvalidRequest        Code = "generic.invalid_request"
	NotFound              Code = "generic.not_found"
	RequestCanceled       Code = "generic.request_canceled"
	RequestTimeout        Code = "generic.request_timeout"
	Internal              Code = "generic.internal_error"
)

var genericCodes = []Code{
	InvalidValueCode,
	InvalidTransitionCode,
	InvalidRequest,
	NotFound,
	RequestCanceled,
	RequestTimeout,
	Internal,
}

// 项目/工作区域：项目、工作区与乐观并发相关的冲突。
const (
	ProjectWorkspaceInvalid Code = "project.workspace_invalid"
	LeaseConflict           Code = "project.lease_conflict"
	RevisionConflict        Code = "project.revision_conflict"
)

var projectCodes = []Code{
	ProjectWorkspaceInvalid,
	LeaseConflict,
	RevisionConflict,
}

// Agent 域：Agent 生命周期、工作项、投递与请求幂等相关错误。
const (
	AgentExecuting   Code = "agent.executing"
	AgentUnavailable Code = "agent.unavailable"
	AlreadySettled   Code = "agent.already_settled"

	WorkQueueEmpty  Code = "work.queue_empty"
	WorkItemActive  Code = "work.item_active"
	RequestNotFound Code = "request.not_found"
	RequestConflict Code = "request.conflict"
)

var agentCodes = []Code{
	AgentExecuting,
	AgentUnavailable,
	AlreadySettled,
	WorkQueueEmpty,
	WorkItemActive,
	RequestNotFound,
	RequestConflict,
}

// 执行运行域：Agent 执行运行时的失败分类。
const (
	ExecutionBusy          Code = "execution.busy"
	ExecutionContract      Code = "execution.contract_error"
	ExecutionPolicyBlocked Code = "execution.policy_blocked"
	ExecutionApproval      Code = "execution.approval_required"
	ExecutionStorage       Code = "execution.storage_error"
	ExecutionProvider      Code = "execution.provider_error"
	ExecutionTool          Code = "execution.tool_error"
	ExecutionResourceLimit Code = "execution.resource_limit"
	ExecutionClosed        Code = "execution.closed"
	ExecutionInterrupted   Code = "execution.interrupted"
)

var executionCodes = []Code{
	ExecutionBusy,
	ExecutionContract,
	ExecutionPolicyBlocked,
	ExecutionApproval,
	ExecutionStorage,
	ExecutionProvider,
	ExecutionTool,
	ExecutionResourceLimit,
	ExecutionClosed,
	ExecutionInterrupted,
}

// 平台域：模型配置与编排启动状态。
const (
	ModelNotConfigured Code = "model.not_configured"
	NotReady           Code = "orchestration.not_ready"
)

var platformCodes = []Code{
	ModelNotConfigured,
	NotReady,
}

// Codes 返回全部业务码，供边界映射的穷举校验使用。
func Codes() []Code {
	codes := make([]Code, 0, len(genericCodes)+len(projectCodes)+len(agentCodes)+len(executionCodes)+len(platformCodes))
	codes = append(codes, genericCodes...)
	codes = append(codes, projectCodes...)
	codes = append(codes, agentCodes...)
	codes = append(codes, executionCodes...)
	codes = append(codes, platformCodes...)
	return codes
}

// 领域哨兵错误统一承载业务码，app 层据业务码翻译为前端错误码。
var (
	ErrInvalidValue      = New(InvalidValueCode, "")
	ErrInvalidTransition = New(InvalidTransitionCode, "")
	ErrLeaseConflict     = New(LeaseConflict, "")
	ErrAlreadySettled    = New(AlreadySettled, "")
	ErrWorkQueueEmpty    = New(WorkQueueEmpty, "")
	ErrWorkItemActive    = New(WorkItemActive, "")
	ErrAgentExecuting    = New(AgentExecuting, "")
	ErrAgentUnavailable  = New(AgentUnavailable, "")
	ErrRequestNotFound   = New(RequestNotFound, "")
	ErrRequestConflict   = New(RequestConflict, "")
	ErrRevisionConflict  = New(RevisionConflict, "")
	ErrNotFound          = New(NotFound, "")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidValue }

// EmptyID reports whether an identifier is blank after trimming whitespace.
func EmptyID(value string) bool { return strings.TrimSpace(value) == "" }

// InvalidValue builds a validation error for one domain field.
func InvalidValue(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// FieldError prefixes an existing validation error with the outer field that
// contains it, so nested validation reports the full path.
func FieldError(field string, err error) error {
	if err == nil {
		return nil
	}
	return InvalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}

type TransitionError struct {
	Entity string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s cannot transition from %q to %q", e.Entity, e.From, e.To)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }

// InvalidTransition builds a transition error for one domain state machine.
func InvalidTransition(entity, from, to string) error {
	return &TransitionError{Entity: entity, From: from, To: to}
}
