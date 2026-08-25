package runtime

import (
	"context"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

// ExecutionLifecycle acknowledges transcript receipts for one activation.
// The caller supplies it at activation time so a long-lived runtime cannot
// retain product-state mutation ownership between executions.
type ExecutionLifecycle interface {
	ConfirmExecutionStart(context.Context, coresession.ExecutionStartReceipt) error
	SettleRuntimeExecution(
		context.Context,
		domain.AgentExecutionID,
		domain.ExecutionOutcome,
		domain.ExecutionFailureCode,
	) error
}
