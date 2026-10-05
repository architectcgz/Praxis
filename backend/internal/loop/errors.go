package loop

import "praxis/internal/contracts"

// ErrorCode 是运行时失败的稳定业务分类，供编排与 app 层映射使用。
type ErrorCode = contracts.Code

const (
	ErrorBusy             = contracts.TurnBusy
	ErrorContract         = contracts.TurnContract
	ErrorPolicyBlocked    = contracts.TurnPolicyBlocked
	ErrorApprovalRequired = contracts.TurnApproval
	ErrorStorage          = contracts.TurnStorage
	ErrorProvider         = contracts.TurnProvider
	ErrorTool             = contracts.TurnTool
	ErrorInterrupted      = contracts.TurnInterrupted
	ErrorResourceLimit    = contracts.TurnResourceLimit
	ErrorClosed           = contracts.TurnClosed
)

// RuntimeError 复用业务错误载体，低敏感、可安全分类。
type RuntimeError = contracts.Error
