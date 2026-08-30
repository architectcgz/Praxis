package orchestrate

import (
	"context"
	"errors"
	"strings"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

// MarkExecutionRunning records the product transition after runtime has
// reconciled its execution-start receipt. It is idempotent for repeated activation.
func (o *AgentOrchestrator) MarkExecutionRunning(
	ctx context.Context,
	executionID domain.AgentExecutionID,
) error {
	if ctx == nil {
		return errors.New("mark running context is required")
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		if execution.Status == domain.ExecutionRunning {
			return nil
		}
		if execution.Status != domain.ExecutionStarting {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := execution.MarkRunning(o.clock.Now()); err != nil {
			return err
		}
		return o.executions.Save(txCtx, execution)
	})
}

// ConfirmExecutionStart accepts a JSONL receipt after the runtime has fsynced
// both execution_started and the source-request message. Only then may SQLite drop
// its temporary StartContent copy.
func (o *AgentOrchestrator) ConfirmExecutionStart(
	ctx context.Context,
	receipt coresession.ExecutionStartReceipt,
) error {
	if ctx == nil {
		return errors.New("execution start receipt context is required")
	}
	if strings.TrimSpace(receipt.ExecutionID.String()) == "" || strings.TrimSpace(receipt.EntryID) == "" {
		return commandError(CommandErrorInvalidRequest)
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, receipt.ExecutionID)
		if err != nil {
			return err
		}
		if execution.RequestID != receipt.RequestID {
			return commandError(CommandErrorInvalidRequest)
		}
		if execution.Status == domain.ExecutionStarting {
			if err := execution.MarkRunning(o.clock.Now()); err != nil {
				return err
			}
		}
		if execution.Status != domain.ExecutionRunning && execution.Status != domain.ExecutionSettling {
			return commandError(CommandErrorAgentUnavailable)
		}
		if execution.StartContent != "" {
			if err := execution.ClearStartContent(receipt.InputDigest); err != nil {
				return err
			}
		}
		return o.executions.Save(txCtx, execution)
	})
}

type ExecutionSettlement struct {
	ExecutionID domain.AgentExecutionID
	Outcome     domain.ExecutionOutcome
	FailureCode domain.ExecutionFailureCode
}

// SettleExecution is called only after the runtime has fsynced its transcript
// settlement receipt. It applies Agent state and control request completion in
// the same SQLite transaction.
func (o *AgentOrchestrator) SettleExecution(ctx context.Context, settlement ExecutionSettlement) error {
	if ctx == nil {
		return errors.New("settlement context is required")
	}
	if strings.TrimSpace(settlement.ExecutionID.String()) == "" || !isKnownExecutionOutcome(settlement.Outcome) {
		return commandError(CommandErrorInvalidRequest)
	}
	var settledAgentID domain.AgentID
	var advanceQueue bool
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, settlement.ExecutionID)
		if err != nil {
			return err
		}
		if execution.Status == domain.ExecutionSettled {
			return nil
		}
		at := o.clock.Now()
		if execution.Status == domain.ExecutionStarting {
			if err := execution.MarkRunning(at); err != nil {
				return err
			}
		}
		if execution.Status == domain.ExecutionRunning {
			if err := execution.BeginSettlement(at); err != nil {
				return err
			}
		}
		if err := execution.Settle(settlement.Outcome, settlement.FailureCode, at); err != nil {
			return err
		}
		agent, err := o.agents.Get(txCtx, execution.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != execution.ID {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := agent.Settle(settlement.Outcome, at); err != nil {
			return err
		}
		if execution.WorkItemID != "" {
			work, err := o.queuedWork.Get(txCtx, execution.WorkItemID)
			if err != nil {
				return err
			}
			if work.AgentID != agent.ID || work.ExecutionID != execution.ID {
				return commandError(CommandErrorAgentUnavailable)
			}
			if err := work.Settle(execution.ID, settlement.Outcome, settlement.FailureCode, at); err != nil {
				return err
			}
			if err := o.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
			advanceQueue = settlement.Outcome == domain.ExecutionCompleted ||
				settlement.Outcome == domain.ExecutionYielded
		}
		controls, err := o.controls.ListOpenByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		for index := range controls {
			control := &controls[index]
			if control.TargetExecutionID != execution.ID {
				continue
			}
			if control.Kind == domain.ControlClose {
				if err := agent.Close(at); err != nil {
					return err
				}
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
			if err := o.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		settledAgentID = agent.ID
		return o.agents.Save(txCtx, agent)
	})
	if err != nil || !advanceQueue {
		return err
	}
	_, _ = o.StartNextQueuedWork(context.WithoutCancel(ctx), settledAgentID)
	return nil
}

// SettleRuntimeExecution is the narrow runtime callback after its independent
// JSONL settlement context has durably recorded the final receipt.
func (o *AgentOrchestrator) SettleRuntimeExecution(
	ctx context.Context,
	executionID domain.AgentExecutionID,
	outcome domain.ExecutionOutcome,
	failureCode domain.ExecutionFailureCode,
) error {
	return o.SettleExecution(ctx, ExecutionSettlement{
		ExecutionID: executionID,
		Outcome:     outcome,
		FailureCode: failureCode,
	})
}

func isKnownExecutionOutcome(outcome domain.ExecutionOutcome) bool {
	switch outcome {
	case domain.ExecutionCompleted, domain.ExecutionYielded, domain.ExecutionPaused,
		domain.ExecutionFailed, domain.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
