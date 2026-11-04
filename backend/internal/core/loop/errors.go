package loop

import (
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	"praxis/internal/errorcode"
)

const (
	ErrBusy             errorcode.Code = "execution.busy"
	ErrContract         errorcode.Code = "execution.contract_error"
	ErrPolicyBlocked    errorcode.Code = "execution.policy_blocked"
	ErrApprovalRequired errorcode.Code = "execution.approval_required"
	ErrStorage          errorcode.Code = "execution.storage_error"
	ErrProvider         errorcode.Code = "execution.provider_error"
	ErrTool             errorcode.Code = "execution.tool_error"
	ErrInterrupted      errorcode.Code = "interrupted"
	ErrResourceLimit    errorcode.Code = "execution.resource_limit"
)

// Failure 保存执行能力边界的失败分类，供执行服务编排和上层结算使用。
type Failure struct {
	code    FailureCode
	cause   error
	message string
}

func (f *Failure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *Failure) Unwrap() error { return f.cause }

func (f *Failure) Message() string { return f.message }

func fail(code FailureCode, operationCode errorcode.Code, message string) error {
	return &Failure{
		code:    code,
		cause:   fmt.Errorf("%s: %w", message, operationCode),
		message: message,
	}
}

// NewFailure 为执行能力边界保留明确的失败分类，供 Loop 和上层结算统一处理。
func NewFailure(code FailureCode, cause error) error {
	return &Failure{code: code, cause: cause}
}

// NewModelFailure 保留已有失败分类，将上下文容量错误与模型依赖错误分开。
func NewModelFailure(err error) error {
	if _, ok := errors.AsType[*Failure](err); ok {
		return err
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return NewFailure(FailureResourceLimit, fmt.Errorf("model context window exceeded: %w", err))
	}
	return NewFailure(FailureProvider, err)
}

// FailureClassification 供业务结算边界读取错误分类。
func FailureClassification(err error) FailureCode {
	if f, ok := errors.AsType[*Failure](err); ok {
		return f.code
	}
	return FailureInternal
}
