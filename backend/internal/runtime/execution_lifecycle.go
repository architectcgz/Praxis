package runtime

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
)

// ExecutionStartReceipt is the durable acknowledgement returned by the
// Agent-owned transcript after the execution-start entry is fsynced. It lives
// in the core runtime boundary so the transcript adapter depends on the
// runtime contract instead of the reverse.
type ExecutionStartReceipt struct {
	ExecutionID contracts.AgentExecutionID
	RequestID   contracts.RequestID
	EntryID     string
	Sequence    uint64
	InputDigest string
}

// ExecutionLifecycle acknowledges transcript receipts for one activation.
// The caller supplies it at activation time so a long-lived runtime cannot
// retain product-state mutation ownership between executions.
type ExecutionLifecycle interface {
	ConfirmExecutionStart(context.Context, ExecutionStartReceipt) error
	SettleRuntimeExecution(
		context.Context,
		contracts.AgentExecutionID,
		executionmodel.ExecutionOutcome,
		contracts.ExecutionFailureCode,
	) error
}
