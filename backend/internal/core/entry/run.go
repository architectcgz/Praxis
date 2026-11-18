package entry

import "praxis/internal/core/entry/record"

type (
	TurnRecord     = record.TurnRecord
	RunFailureCode = record.RunFailureCode
	RunResult      = record.RunResult
	RunStatus      = record.RunStatus
	RunOutcome     = record.RunOutcome
	RunSnapshot    = record.RunSnapshot
	RunConclusion  = record.RunConclusion
)

const (
	RunFailureTimeout             = record.RunFailureTimeout
	RunFailureProviderUnavailable = record.RunFailureProviderUnavailable
	RunFailureProvider            = record.RunFailureProvider
	RunFailureTool                = record.RunFailureTool
	RunFailurePolicyBlocked       = record.RunFailurePolicyBlocked
	RunFailureApprovalRequired    = record.RunFailureApprovalRequired
	RunFailureResourceLimit       = record.RunFailureResourceLimit
	RunFailureStorage             = record.RunFailureStorage
	RunFailureContract            = record.RunFailureContract
	RunFailureBusy                = record.RunFailureBusy
	RunFailureInterrupted         = record.RunFailureInterrupted
	RunFailureRequestCanceled     = record.RunFailureRequestCanceled
	RunFailureCancelled           = record.RunFailureCancelled
	RunFailureInternal            = record.RunFailureInternal
	RunFailureInvalidOutcome      = record.RunFailureInvalidOutcome
	RunStarting                   = record.RunStarting
	RunRunning                    = record.RunRunning
	RunEnding                     = record.RunEnding
	RunEnded                      = record.RunEnded
	OutcomeCompleted              = record.OutcomeCompleted
	OutcomeYielded                = record.OutcomeYielded
	OutcomeFailed                 = record.OutcomeFailed
	OutcomeInterrupted            = record.OutcomeInterrupted
	MaxRunFailureMessageRunes     = record.MaxRunFailureMessageRunes
)

func (e Entry) IsRun() bool { return e.ExecutionResult() != nil }
