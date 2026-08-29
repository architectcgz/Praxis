package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/core/domain"
)

// QueueWorkRequest contains one independent task. The caller retains ID for
// retries; it is neither a user-input RequestID nor a transcript message.
type QueueWorkRequest struct {
	ID              domain.WorkItemID
	AgentID         domain.AgentID
	Prompt          string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

type QueueWorkResult struct {
	Work            domain.QueuedWork
	ExistingWork    bool
	ActivationError string
}

// EnqueueWork durably stores an independent task even while its Agent has an
// active execution. It only starts a task when the Agent is independently
// eligible, so it cannot bypass SendInput admission or alter a transcript.
func (o *AgentOrchestrator) EnqueueWork(ctx context.Context, request QueueWorkRequest) (QueueWorkResult, error) {
	if ctx == nil {
		return QueueWorkResult{}, errors.New("enqueue work context is required")
	}
	if !o.Ready() {
		return QueueWorkResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.ID.String()) == "" || strings.TrimSpace(request.AgentID.String()) == "" ||
		strings.TrimSpace(request.Prompt) == "" {
		return QueueWorkResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return QueueWorkResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	result := QueueWorkResult{}
	startEligible := false
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.queuedWork.Get(txCtx, request.ID)
		if err == nil {
			if existing.AgentID != request.AgentID || existing.Prompt != strings.TrimSpace(request.Prompt) ||
				existing.Input.Runtime != request.RuntimeSnapshot {
				return commandError(CommandErrorInvalidRequest)
			}
			result.Work = existing
			result.ExistingWork = true
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		sequence, err := o.queuedWork.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		work, err := domain.NewQueuedWork(
			request.ID,
			agent.SessionID,
			agent.ID,
			sequence,
			request.Prompt,
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: agent.GrantID,
				Runtime:           request.RuntimeSnapshot,
			},
			o.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := o.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		result.Work = work
		startEligible = agent.State == domain.AgentIdle || agent.State == domain.AgentWaiting ||
			agent.State == domain.AgentFailed || agent.State == domain.AgentClosed
		return nil
	})
	if err != nil || result.ExistingWork || !startEligible {
		return result, err
	}
	started, err := o.StartNextQueuedWork(context.WithoutCancel(ctx), request.AgentID)
	if err != nil {
		result.ActivationError = err.Error()
	} else if started.ActivationError != "" {
		result.ActivationError = started.ActivationError
	}
	return result, nil
}

type QueuedWorkStartResult struct {
	Work            domain.QueuedWork
	Execution       domain.AgentExecution
	Started         bool
	ActivationError string
}

// StartNextQueuedWork applies the default FIFO policy. It is safe for both a
// post-command wakeup and recovery because the queue and execution transition
// share one transaction and scheduler activation is only an optimization.
func (o *AgentOrchestrator) StartNextQueuedWork(
	ctx context.Context,
	agentID domain.AgentID,
) (QueuedWorkStartResult, error) {
	if ctx == nil {
		return QueuedWorkStartResult{}, errors.New("start queued work context is required")
	}
	if strings.TrimSpace(agentID.String()) == "" {
		return QueuedWorkStartResult{}, commandError(CommandErrorInvalidRequest)
	}
	result := QueuedWorkStartResult{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := o.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
			return nil
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			if hasCommandErrorCode(err, CommandErrorAgentUnavailable) {
				return nil
			}
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			if hasCommandErrorCode(err, CommandErrorAgentUnavailable) {
				return nil
			}
			return err
		}
		work, err := o.queuedWork.FindNextPendingByAgent(txCtx, agent.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewQueuedWorkExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			work.ID,
			work.Input,
			at,
		)
		if err != nil {
			return err
		}
		if err := work.Start(execution.ID, at); err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Work = work
		result.Execution = execution
		result.Started = true
		return nil
	})
	if err != nil || !result.Started || o.activator == nil {
		return result, err
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), agentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}
