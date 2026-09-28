package loop

import "praxis/internal/contracts"

// ErrorCode 是运行时失败的稳定业务分类，供编排与 app 层映射使用。
type ErrorCode = contracts.Code

const (
	ErrorBusy             = contracts.ExecutionBusy
	ErrorContract         = contracts.ExecutionContract
	ErrorPolicyBlocked    = contracts.ExecutionPolicyBlocked
	ErrorApprovalRequired = contracts.ExecutionApproval
	ErrorStorage          = contracts.ExecutionStorage
	ErrorProvider         = contracts.ExecutionProvider
	ErrorTool             = contracts.ExecutionTool
	ErrorInterrupted      = contracts.ExecutionInterrupted
	ErrorResourceLimit    = contracts.ExecutionResourceLimit
	ErrorClosed           = contracts.ExecutionClosed
)

// RuntimeError 复用业务错误载体，低敏感、可安全分类。
type RuntimeError = contracts.Error

// IsCode 判断错误链是否包含指定的运行时错误码。
func IsCode(err error, code ErrorCode) bool {
	found, ok := contracts.CodeOf(err)
	return ok && found == code
}
