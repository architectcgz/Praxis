// Package control owns durable Pause and Close command handling.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	commandprotocol "praxis/internal/command"
	domainagent "praxis/internal/domain/agent"
	domaincommand "praxis/internal/domain/command"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/persistence"
	"praxis/internal/system"
)

// Readiness controls command admission while durable startup recovery runs.
type Readiness interface {
	Ready() bool
}

// RuntimeCancellation sends a durable control request to a process-local
// runtime after the transaction that recorded it has committed.
type RuntimeCancellation interface {
	Cancel(context.Context, domainfoundation.AgentID, domainfoundation.AgentExecutionID, domainexecution.ExecutionOutcome) error
}

// Config contains the ports required by the control application service.
type Config struct {
	Transactions    persistence.TxRunner
	Agents          persistence.AgentRepository
	Executions      persistence.AgentExecutionRepository
	Controls        persistence.AgentControlRequestRepository
	CommandReceipts persistence.CommandReceiptRepository
	Events          persistence.EventRepository
	Readiness       Readiness
	Canceller       RuntimeCancellation
	Clock           system.Clock
	IDs             system.IDGenerator
}

// Service owns the control-request transaction and post-commit cancellation.
type Service struct {
	tx              persistence.TxRunner
	agents          persistence.AgentRepository
	executions      persistence.AgentExecutionRepository
	controls        persistence.AgentControlRequestRepository
	commandReceipts persistence.CommandReceiptRepository
	events          persistence.EventRepository
	readiness       Readiness
	canceller       RuntimeCancellation
	clock           system.Clock
	ids             system.IDGenerator
}

// RequestParams identifies one Pause or Close command.
type RequestParams struct {
	RequestID domainfoundation.AgentControlRequestID
	AgentID   domainfoundation.AgentID
	Kind      domainworkflow.AgentControlKind
}

// RequestResult returns the durable control request and cancellation outcome.
type RequestResult struct {
	Request           domainworkflow.AgentControlRequest
	ExistingRequest   bool
	CancellationError string
}

// NewService creates the control application service.
func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "transactions", value: config.Transactions},
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "controls", value: config.Controls},
		{name: "command receipts", value: config.CommandReceipts},
		{name: "events", value: config.Events},
		{name: "readiness", value: config.Readiness},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("control service %s is required", required.name)
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = system.UTCClock{}
	}
	ids := config.IDs
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	return &Service{
		tx:              config.Transactions,
		agents:          config.Agents,
		executions:      config.Executions,
		controls:        config.Controls,
		commandReceipts: config.CommandReceipts,
		events:          config.Events,
		readiness:       config.Readiness,
		canceller:       config.Canceller,
		clock:           clock,
		ids:             ids,
	}, nil
}

// RequestControl records Pause or Close before signaling runtime cancellation.
// A caller timeout cannot retract the committed control request.
func (s *Service) RequestControl(ctx context.Context, params RequestParams) (RequestResult, error) {
	if ctx == nil {
		return RequestResult{}, errors.New("control request context is required")
	}
	if !s.readiness.Ready() {
		return RequestResult{}, commandprotocol.NewError(commandprotocol.ErrorNotReady)
	}
	if strings.TrimSpace(params.RequestID.String()) == "" || strings.TrimSpace(params.AgentID.String()) == "" ||
		(params.Kind != domainworkflow.ControlPause && params.Kind != domainworkflow.ControlClose) {
		return RequestResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	var result RequestResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		digest := commandprotocol.ArgumentsDigest(struct {
			AgentID domainfoundation.AgentID
			Kind    domainworkflow.AgentControlKind
		}{params.AgentID, params.Kind})
		if _, found, err := commandprotocol.FindReceipt(txCtx, s.commandReceipts, domainfoundation.RequestID(params.RequestID), "request_control", digest); err != nil {
			return err
		} else if found {
			control, err := s.controls.Get(txCtx, params.RequestID)
			if err != nil {
				return err
			}
			result = RequestResult{Request: control, ExistingRequest: true}
			return nil
		}
		existing, err := s.controls.Get(txCtx, params.RequestID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.Kind != params.Kind {
				return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
			}
			result = RequestResult{Request: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		control, err := domainworkflow.NewAgentControlRequest(params.RequestID, agent.ID, agent.CurrentExecutionID, params.Kind, at)
		if err != nil {
			return err
		}
		if agent.State == domainagent.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == domainagent.AgentPausing {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		} else if params.Kind == domainworkflow.ControlClose {
			if err := agent.Close(at); err != nil {
				return err
			}
			if err := control.MarkApplied(at); err != nil {
				return err
			}
		} else {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		if err := s.controls.Save(txCtx, control); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		eventType := domainfoundation.EventAgentPausing
		if params.Kind == domainworkflow.ControlClose {
			eventType = domainfoundation.EventAgentClosed
		}
		event := domainfoundation.DomainEvent{
			ID:               domainfoundation.EventID(s.ids.New("event")),
			Type:             eventType,
			AgentID:          agent.ID,
			AgentExecutionID: control.TargetExecutionID,
			OccurredAt:       at.UTC(),
		}
		if err := s.events.Append(txCtx, event); err != nil {
			return err
		}
		payload, _ := json.Marshal(struct{ RequestID string }{control.ID.String()})
		if err := s.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
			RequestID: domainfoundation.RequestID(params.RequestID), Command: "request_control",
			ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: at,
		}); err != nil {
			return err
		}
		result.Request = control
		return nil
	})
	if err != nil {
		return RequestResult{}, err
	}
	if result.ExistingRequest || result.Request.Status == domainworkflow.ControlApplied || s.canceller == nil {
		return result, nil
	}
	if err := s.canceller.Cancel(
		context.WithoutCancel(ctx),
		result.Request.AgentID,
		result.Request.TargetExecutionID,
		cancellationOutcome(result.Request.Kind),
	); err != nil {
		result.CancellationError = err.Error()
	}
	return result, nil
}

// ApplyControlRequest finishes a control whose target execution is already
// durable-settled. Recovery calls this after lost runtime notifications.
func (s *Service) ApplyControlRequest(ctx context.Context, requestID domainfoundation.AgentControlRequestID) error {
	if ctx == nil {
		return errors.New("apply control request context is required")
	}
	if strings.TrimSpace(requestID.String()) == "" {
		return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		control, err := s.controls.Get(txCtx, requestID)
		if err != nil {
			return err
		}
		if control.Status == domainworkflow.ControlApplied {
			return nil
		}
		agent, err := s.agents.Get(txCtx, control.AgentID)
		if err != nil {
			return err
		}
		if control.TargetExecutionID != "" {
			execution, err := s.executions.Get(txCtx, control.TargetExecutionID)
			if err != nil {
				return err
			}
			if execution.Active() {
				return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
			}
		}
		at := s.clock.Now()
		if control.Kind == domainworkflow.ControlClose {
			if agent.State == domainagent.AgentExecuting || agent.State == domainagent.AgentPausing {
				return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
			}
			if err := agent.Close(at); err != nil {
				return err
			}
		}
		if err := control.MarkApplied(at); err != nil {
			return err
		}
		if err := s.controls.Save(txCtx, control); err != nil {
			return err
		}
		return s.agents.Save(txCtx, agent)
	})
}

func cancellationOutcome(kind domainworkflow.AgentControlKind) domainexecution.ExecutionOutcome {
	if kind == domainworkflow.ControlPause {
		return domainexecution.ExecutionPaused
	}
	return domainexecution.ExecutionInterrupted
}
