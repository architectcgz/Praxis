package loop

import (
	"errors"
	"fmt"
	taskmodel "praxis/internal/agent"
	"praxis/internal/fault"
)

// ErrorCode 是运行时失败的稳定业务分类，供编排与 app 层映射使用。
type ErrorCode = fault.Code

const (
	ErrorBusy             = taskmodel.ExecutionBusy
	ErrorContract         = taskmodel.ExecutionContract
	ErrorPolicyBlocked    = taskmodel.ExecutionPolicyBlocked
	ErrorApprovalRequired = taskmodel.ExecutionApproval
	ErrorStorage          = taskmodel.ExecutionStorage
	ErrorProvider         = taskmodel.ExecutionProvider
	ErrorTool             = taskmodel.ExecutionTool
	ErrorInterrupted      = taskmodel.ExecutionInterrupted
	ErrorResourceLimit    = taskmodel.ExecutionResourceLimit
)

// RuntimeError 复用业务错误载体，低敏感、可安全分类。
type RuntimeError = fault.Error

// taskFailure 携带引擎错误对应的持久化结束结果。
// taskResult 在统一的 run 边界把它转换为返回值。
type taskFailure struct {
	outcome taskmodel.ExecutionOutcome
	code    taskmodel.ExecutionFailureCode
	cause   error
}

func (f *taskFailure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *taskFailure) Unwrap() error { return f.cause }

func fail(code taskmodel.ExecutionFailureCode, runtimeCode ErrorCode, message string) error {
	return &taskFailure{
		outcome: taskmodel.ExecutionFailed,
		code:    code,
		cause:   &RuntimeError{Code: runtimeCode, Message: message},
	}
}

func failCause(code taskmodel.ExecutionFailureCode, cause error) error {
	return &taskFailure{outcome: taskmodel.ExecutionFailed, code: code, cause: cause}
}

// modelFailure 在 loop 编排边界分类模型和压缩失败；Provider 校验最终编码请求的窗口。
func modelFailure(err error) error {
	var failure *taskFailure
	if errors.As(err, &failure) {
		return err
	}
	var windowErr *ContextWindowExceededError
	if errors.As(err, &windowErr) {
		return fail(taskmodel.ExecutionFailureResourceLimit, ErrorResourceLimit, "model context window exceeded")
	}
	return failCause(taskmodel.ExecutionFailureProvider, err)
}

func taskResult(err error) (taskmodel.ExecutionOutcome, taskmodel.ExecutionFailureCode, error) {
	var failure *taskFailure
	if errors.As(err, &failure) {
		return failure.outcome, failure.code, failure.cause
	}
	return failedTask(err)
}

func failedTask(err error) (taskmodel.ExecutionOutcome, taskmodel.ExecutionFailureCode, error) {
	if err == nil {
		err = errors.New("loop failed")
	}
	return taskmodel.ExecutionFailed, taskmodel.ExecutionFailureRuntimeFailed, fmt.Errorf("loop: %w", err)
}
