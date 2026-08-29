package orchestrate

import (
	"context"
	"errors"
	"strings"

	"praxis/internal/core/domain"
)

type ControlRequest struct {
	ID      domain.AgentControlRequestID
	AgentID domain.AgentID
	Kind    domain.AgentControlKind
}

type ControlResult struct {
	Request           domain.AgentControlRequest
	ExistingRequest   bool
	CancellationError string
}

// RequestControl durably records Pause or Close before it signals runtime
// cancellation. A caller timeout cannot retract the committed control request.
func (o *AgentOrchestrator) RequestControl(ctx context.Context, request ControlRequest) (ControlResult, error) {
	if ctx == nil {
		return ControlResult{}, errors.New("control request context is required")
	}
	if !o.Ready() {
		return ControlResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.ID.String()) == "" || strings.TrimSpace(request.AgentID.String()) == "" ||
		(request.Kind != domain.ControlPause && request.Kind != domain.ControlClose) {
		return ControlResult{}, commandError(CommandErrorInvalidRequest)
	}
	var result ControlResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.controls.Get(txCtx, request.ID)
		if err == nil {
			if existing.AgentID != request.AgentID || existing.Kind != request.Kind {
				return commandError(CommandErrorInvalidRequest)
			}
			result = ControlResult{Request: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		at := o.clock.Now()
		control, err := domain.NewAgentControlRequest(request.ID, agent.ID, agent.CurrentExecutionID, request.Kind, at)
		if err != nil {
			return err
		}
		if agent.State == domain.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == domain.AgentPausing {
			return commandError(CommandErrorAgentUnavailable)
		} else if request.Kind == domain.ControlClose {
			if err := agent.Close(at); err != nil {
				return err
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
		} else {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.controls.Save(txCtx, control); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Request = control
		return nil
	})
	if err != nil {
		return ControlResult{}, err
	}
	if result.ExistingRequest || result.Request.Status == domain.ControlApplied || o.canceller == nil {
		return result, nil
	}
	if err := o.canceller.Cancel(
		context.WithoutCancel(ctx),
		result.Request.AgentID,
		result.Request.TargetExecutionID,
		controlCancellationOutcome(result.Request.Kind),
	); err != nil {
		result.CancellationError = err.Error()
	}
	return result, nil
}

// ApplyControlRequest finishes a requested control whose target execution is
// already durable-settled. Recovery uses this path for controls that were
// committed before a runtime callback or process notification was lost.
func (o *AgentOrchestrator) ApplyControlRequest(
	ctx context.Context,
	requestID domain.AgentControlRequestID,
) error {
	if ctx == nil {
		return errors.New("apply control request context is required")
	}
	if strings.TrimSpace(requestID.String()) == "" {
		return commandError(CommandErrorInvalidRequest)
	}
	return o.tx.InTx(ctx, func(txCtx context.Context) error {
		control, err := o.controls.Get(txCtx, requestID)
		if err != nil {
			return err
		}
		if control.Status == domain.ControlApplied {
			return nil
		}
		agent, err := o.agents.Get(txCtx, control.AgentID)
		if err != nil {
			return err
		}
		if control.TargetExecutionID != "" {
			execution, err := o.executions.Get(txCtx, control.TargetExecutionID)
			if err != nil {
				return err
			}
			if execution.Active() {
				return commandError(CommandErrorAgentUnavailable)
			}
		}
		at := o.clock.Now()
		if control.Kind == domain.ControlClose {
			if agent.State == domain.AgentExecuting || agent.State == domain.AgentPausing {
				return commandError(CommandErrorAgentUnavailable)
			}
			if err := agent.Close(at); err != nil {
				return err
			}
		}
		if err := control.MarkApplied(at); err != nil {
			return err
		}
		if err := o.controls.Save(txCtx, control); err != nil {
			return err
		}
		return o.agents.Save(txCtx, agent)
	})
}

func controlCancellationOutcome(kind domain.AgentControlKind) domain.ExecutionOutcome {
	if kind == domain.ControlPause {
		return domain.ExecutionPaused
	}
	return domain.ExecutionInterrupted
}
