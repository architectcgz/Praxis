package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	domainagent "praxis/internal/core/domain/agent"
	domaincommand "praxis/internal/core/domain/command"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"
)

type ControlRequest struct {
	RequestID domainfoundation.AgentControlRequestID
	AgentID   domainfoundation.AgentID
	Kind      domainworkflow.AgentControlKind
}

type ControlResult struct {
	Request           domainworkflow.AgentControlRequest
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
	if strings.TrimSpace(request.RequestID.String()) == "" || strings.TrimSpace(request.AgentID.String()) == "" ||
		(request.Kind != domainworkflow.ControlPause && request.Kind != domainworkflow.ControlClose) {
		return ControlResult{}, commandError(CommandErrorInvalidRequest)
	}
	var result ControlResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		digest := commandArgumentsDigest(struct {
			AgentID domainfoundation.AgentID
			Kind    domainworkflow.AgentControlKind
		}{request.AgentID, request.Kind})
		if _, found, err := commandReceipt(txCtx, o.commandReceipts, domainfoundation.RequestID(request.RequestID), "request_control", digest); err != nil {
			return err
		} else if found {
			control, err := o.controls.Get(txCtx, request.RequestID)
			if err != nil {
				return err
			}
			result = ControlResult{Request: control, ExistingRequest: true}
			return nil
		}
		existing, err := o.controls.Get(txCtx, request.RequestID)
		if err == nil {
			if existing.AgentID != request.AgentID || existing.Kind != request.Kind {
				return commandError(CommandErrorInvalidRequest)
			}
			result = ControlResult{Request: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		at := o.clock.Now()
		control, err := domainworkflow.NewAgentControlRequest(request.RequestID, agent.ID, agent.CurrentExecutionID, request.Kind, at)
		if err != nil {
			return err
		}
		if agent.State == domainagent.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == domainagent.AgentPausing {
			return commandError(CommandErrorAgentUnavailable)
		} else if request.Kind == domainworkflow.ControlClose {
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
		eventType := domainfoundation.EventAgentPausing
		if request.Kind == domainworkflow.ControlClose {
			eventType = domainfoundation.EventAgentClosed
		}
		event := o.newEvent(eventType, at)
		event.AgentID, event.AgentExecutionID = agent.ID, control.TargetExecutionID
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		if o.commandReceipts != nil {
			payload, _ := json.Marshal(struct{ RequestID string }{control.ID.String()})
			if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
				RequestID: domainfoundation.RequestID(request.RequestID), Command: "request_control",
				ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: at,
			}); err != nil {
				return err
			}
		}
		result.Request = control
		return nil
	})
	if err != nil {
		return ControlResult{}, err
	}
	if result.ExistingRequest || result.Request.Status == domainworkflow.ControlApplied || o.canceller == nil {
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
	requestID domainfoundation.AgentControlRequestID,
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
		if control.Status == domainworkflow.ControlApplied {
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
		if control.Kind == domainworkflow.ControlClose {
			if agent.State == domainagent.AgentExecuting || agent.State == domainagent.AgentPausing {
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

func controlCancellationOutcome(kind domainworkflow.AgentControlKind) domainexecution.ExecutionOutcome {
	if kind == domainworkflow.ControlPause {
		return domainexecution.ExecutionPaused
	}
	return domainexecution.ExecutionInterrupted
}
