package loop

import (
	"errors"
	"fmt"
	"praxis/internal/fault"
)

// ErrorCode 是运行时失败的稳定业务分类，供编排与 app 层映射使用。
type ErrorCode = fault.Code

const (
	ErrorBusy             ErrorCode = "execution.busy"
	ErrorContract         ErrorCode = "execution.contract_error"
	ErrorPolicyBlocked    ErrorCode = "execution.policy_blocked"
	ErrorApprovalRequired ErrorCode = "execution.approval_required"
	ErrorStorage          ErrorCode = "execution.storage_error"
	ErrorProvider         ErrorCode = "execution.provider_error"
	ErrorTool             ErrorCode = "execution.tool_error"
	ErrorInterrupted      ErrorCode = "interrupted"
	ErrorResourceLimit    ErrorCode = "execution.resource_limit"
)

// OperationError 复用业务错误载体，低敏感、可安全分类。
type OperationError = fault.Error

// Failure 携带引擎错误对应的持久化结束结果。
// loopResult 在统一的 run 边界把它转换为返回值。
type Failure struct {
	outcome EndReason
	code    FailureCode
	cause   error
}

func (f *Failure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *Failure) Unwrap() error { return f.cause }

func fail(code FailureCode, operationCode ErrorCode, message string) error {
	return &Failure{
		outcome: endFailed,
		code:    code,
		cause:   &OperationError{Code: operationCode, Message: message},
	}
}

func failCause(code FailureCode, cause error) error {
	return &Failure{outcome: endFailed, code: code, cause: cause}
}

// modelFailure 在 loop 编排边界分类模型和压缩失败；Provider 校验最终编码请求的窗口。
func modelFailure(err error) error {
	var failure *Failure
	if errors.As(err, &failure) {
		return err
	}
	var windowErr *ContextWindowExceededError
	if errors.As(err, &windowErr) {
		return fail(FailureResourceLimit, ErrorResourceLimit, "model context window exceeded")
	}
	return failCause(FailureProvider, err)
}

func loopResult(err error) (EndReason, FailureCode, error) {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.outcome, failure.code, failure
	}
	return failedLoop(err)
}

func failedLoop(err error) (EndReason, FailureCode, error) {
	if err == nil {
		err = errors.New("loop failed")
	}
	return endFailed, FailureInternal, fmt.Errorf("loop: %w", err)
}

// FailureClassification 供业务结算边界读取错误分类。
func FailureClassification(err error) FailureCode {
	var f *Failure
	if errors.As(err, &f) {
		return f.code
	}
	return FailureInternal
}
