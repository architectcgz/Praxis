// Package control owns durable Pause and Close command handling.
package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	commandprotocol "praxis/internal/command"
	domainagent "praxis/internal/domain/agent"
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
	Transactions persistence.TxRunner
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	Controls     persistence.AgentControlCommandRepository
	Events       persistence.EventRepository
	Readiness    Readiness
	Canceller    RuntimeCancellation
	Clock        system.Clock
	IDs          system.IDGenerator
}

// Service owns the control-request transaction and post-commit cancellation.
type Service struct {
	tx         persistence.TxRunner
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	controls   persistence.AgentControlCommandRepository
	events     persistence.EventRepository
	readiness  Readiness
	canceller  RuntimeCancellation
	clock      system.Clock
	ids        system.IDGenerator
}

// Params identifies one Pause or Close command.
type Params struct {
	CommandID domainfoundation.AgentControlCommandID
	AgentID   domainfoundation.AgentID
}

// Result returns the durable control command and cancellation outcome.
type Result struct {
	Command           domainworkflow.AgentControlCommand
	ExistingCommand   bool
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
		tx:         config.Transactions,
		agents:     config.Agents,
		executions: config.Executions,
		controls:   config.Controls,
		events:     config.Events,
		readiness:  config.Readiness,
		canceller:  config.Canceller,
		clock:      clock,
		ids:        ids,
	}, nil
}

// PauseAgent records a durable pause command before signaling cancellation.
func (s *Service) PauseAgent(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, domainworkflow.AgentControlPause)
}

// CloseAgent records a durable close command before signaling cancellation.
func (s *Service) CloseAgent(ctx context.Context, params Params) (Result, error) {
	return s.apply(ctx, params, domainworkflow.AgentControlClose)
}

// apply records Pause or Close before signaling runtime cancellation. A caller
// timeout cannot retract the committed control command.
func (s *Service) apply(ctx context.Context, params Params, kind domainworkflow.AgentControlKind) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("control command context is required")
	}
	if !s.readiness.Ready() {
		return Result{}, commandprotocol.NewError(commandprotocol.ErrorNotReady)
	}
	if strings.TrimSpace(params.CommandID.String()) == "" || strings.TrimSpace(params.AgentID.String()) == "" ||
		(kind != domainworkflow.AgentControlPause && kind != domainworkflow.AgentControlClose) {
		return Result{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	var result Result
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.controls.Get(txCtx, params.CommandID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.Kind != kind {
				return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
			}
			result = Result{Command: existing, ExistingCommand: true}
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
		control, err := domainworkflow.NewAgentControlCommand(params.CommandID, agent.ID, agent.CurrentExecutionID, kind, at)
		if err != nil {
			return err
		}
		if agent.State == domainagent.AgentExecuting {
			if err := agent.RequestPause(at); err != nil {
				return err
			}
		} else if agent.State == domainagent.AgentPausing {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		} else if kind == domainworkflow.AgentControlClose {
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
		if kind == domainworkflow.AgentControlClose {
			eventType = domainfoundation.EventAgentClosed
		}
		event := domainfoundation.DomainEvent{
			ID:               domainfoundation.EventID(s.ids.New("event")),
			Type:             eventType,
			SessionID:        agent.SessionID,
			AgentID:          agent.ID,
			AgentExecutionID: control.TargetExecutionID,
			OccurredAt:       at.UTC(),
		}
		if err := s.events.Append(txCtx, event); err != nil {
			return err
		}
		result.Command = control
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if result.ExistingCommand || result.Command.Status == domainworkflow.AgentControlApplied || s.canceller == nil {
		return result, nil
	}
	if err := s.canceller.Cancel(
		context.WithoutCancel(ctx),
		result.Command.AgentID,
		result.Command.TargetExecutionID,
		cancellationOutcome(result.Command.Kind),
	); err != nil {
		result.CancellationError = err.Error()
	}
	return result, nil
}

// ApplyPendingAgentControl finishes a control whose target execution is already
// durable-settled. Recovery calls this after lost runtime notifications.
func (s *Service) ApplyPendingAgentControl(ctx context.Context, requestID domainfoundation.AgentControlCommandID) error {
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
		if control.Status == domainworkflow.AgentControlApplied {
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
		if control.Kind == domainworkflow.AgentControlClose {
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
	if kind == domainworkflow.AgentControlPause {
		return domainexecution.ExecutionPaused
	}
	return domainexecution.ExecutionInterrupted
}
