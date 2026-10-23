package loop

import (
	"errors"
	"fmt"
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

// Failure 携带引擎错误对应的持久化结束结果。
// loopResult 在统一的 run 边界把它转换为返回值。
type Failure struct {
	outcome EndReason
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

func fail(code FailureCode, operationCode errorcode.Code, message string) error {
	return &Failure{
		outcome: endFailed,
		code:    code,
		cause:   fmt.Errorf("%s: %w", message, operationCode),
		message: message,
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
		return fail(FailureResourceLimit, ErrResourceLimit, "model context window exceeded")
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
