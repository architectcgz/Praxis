package runtime

import (
	"context"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"

	sessionport "praxis/internal/session"
)

// ExecutionLifecycle acknowledges transcript receipts for one activation.
// The caller supplies it at activation time so a long-lived runtime cannot
// retain product-state mutation ownership between executions.
type ExecutionLifecycle interface {
	ConfirmExecutionStart(context.Context, sessionport.ExecutionStartReceipt) error
	SettleRuntimeExecution(
		context.Context,
		domainfoundation.AgentExecutionID,
		domainexecution.ExecutionOutcome,
		domainexecution.ExecutionFailureCode,
	) error
}
