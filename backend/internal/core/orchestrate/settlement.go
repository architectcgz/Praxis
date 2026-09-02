package orchestrate

import (
	"context"
	"errors"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"
	"strings"

	coresession "praxis/internal/core/session"
)

// MarkExecutionRunning records the product transition after runtime has
// reconciled its execution-start receipt. It is idempotent for repeated activation.
func (o *AgentOrchestrator) MarkExecutionRunning(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
) error {
	if ctx == nil {
		return errors.New("mark running context is required")
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, executionID)
		if err != nil {
			return err
		}
		if execution.Status == domainexecution.ExecutionRunning {
			return nil
		}
		if execution.Status != domainexecution.ExecutionStarting {
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
		if execution.Status == domainexecution.ExecutionStarting {
			if err := execution.MarkRunning(o.clock.Now()); err != nil {
				return err
			}
		}
		if execution.Status != domainexecution.ExecutionRunning && execution.Status != domainexecution.ExecutionSettling {
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
	ExecutionID domainfoundation.AgentExecutionID
	Outcome     domainexecution.ExecutionOutcome
	FailureCode domainexecution.ExecutionFailureCode
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
	var settledAgentID domainfoundation.AgentID
	var advanceQueue bool
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := o.executions.Get(txCtx, settlement.ExecutionID)
		if err != nil {
			return err
		}
		if execution.Status == domainexecution.ExecutionSettled {
			return nil
		}
		at := o.clock.Now()
		if execution.Status == domainexecution.ExecutionStarting {
			if err := execution.MarkRunning(at); err != nil {
				return err
			}
		}
		if execution.Status == domainexecution.ExecutionRunning {
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
			workEvent := o.newEvent(domainfoundation.EventQueuedWorkSettled, at)
			workEvent.SessionID, workEvent.AgentID, workEvent.WorkItemID, workEvent.AgentExecutionID = work.SessionID, work.AgentID, work.ID, execution.ID
			workEvent.Payload = map[string]string{"outcome": string(settlement.Outcome), "failureCode": string(settlement.FailureCode)}
			if err := o.appendEvent(txCtx, workEvent); err != nil {
				return err
			}
			advanceQueue = settlement.Outcome == domainexecution.ExecutionCompleted ||
				settlement.Outcome == domainexecution.ExecutionYielded
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
			if control.Kind == domainworkflow.ControlClose {
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
		eventType := domainfoundation.EventExecutionSettled
		switch settlement.Outcome {
		case domainexecution.ExecutionFailed:
			eventType = domainfoundation.EventAgentFailed
		case domainexecution.ExecutionInterrupted:
			eventType = domainfoundation.EventAgentInterrupted
		case domainexecution.ExecutionPaused:
			eventType = domainfoundation.EventAgentPaused
		}
		event := o.newEvent(eventType, at)
		event.SessionID, event.AgentID, event.AgentExecutionID = execution.SessionID, execution.AgentID, execution.ID
		event.Payload = map[string]string{"outcome": string(settlement.Outcome), "failureCode": string(settlement.FailureCode)}
		if err := o.appendEvent(txCtx, event); err != nil {
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
	executionID domainfoundation.AgentExecutionID,
	outcome domainexecution.ExecutionOutcome,
	failureCode domainexecution.ExecutionFailureCode,
) error {
	return o.SettleExecution(ctx, ExecutionSettlement{
		ExecutionID: executionID,
		Outcome:     outcome,
		FailureCode: failureCode,
	})
}

func isKnownExecutionOutcome(outcome domainexecution.ExecutionOutcome) bool {
	switch outcome {
	case domainexecution.ExecutionCompleted, domainexecution.ExecutionYielded, domainexecution.ExecutionPaused,
		domainexecution.ExecutionFailed, domainexecution.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
