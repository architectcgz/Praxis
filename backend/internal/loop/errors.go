package loop

import "praxis/internal/contracts"

// ErrorCode 是运行时失败的稳定业务分类，供编排与 app 层映射使用。
type ErrorCode = contracts.Code

const (
	ErrorBusy             = contracts.TaskBusy
	ErrorContract         = contracts.TaskContract
	ErrorPolicyBlocked    = contracts.TaskPolicyBlocked
	ErrorApprovalRequired = contracts.TaskApproval
	ErrorStorage          = contracts.TaskStorage
	ErrorProvider         = contracts.TaskProvider
	ErrorTool             = contracts.TaskTool
	ErrorInterrupted      = contracts.TaskInterrupted
	ErrorResourceLimit    = contracts.TaskResourceLimit
)

// RuntimeError 复用业务错误载体，低敏感、可安全分类。
type RuntimeError = contracts.Error
