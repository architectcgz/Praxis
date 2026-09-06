// Package settlement owns durable execution receipt acknowledgement and settlement.
package settlement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	commandprotocol "praxis/internal/command"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/persistence"
	sessionport "praxis/internal/session"
	"praxis/internal/system"
)

type QueueStarter interface {
	StartNextQueuedWork(context.Context, domainfoundation.AgentID) (bool, error)
}

type Config struct {
	Transactions persistence.TxRunner
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	QueuedWork   persistence.QueuedWorkRepository
	Controls     persistence.AgentControlRequestRepository
	Events       persistence.EventRepository
	Clock        system.Clock
	IDs          system.IDGenerator
}

type Service struct {
	tx         persistence.TxRunner
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	queuedWork persistence.QueuedWorkRepository
	controls   persistence.AgentControlRequestRepository
	events     persistence.EventRepository
	clock      system.Clock
	ids        system.IDGenerator
	queueMu    sync.RWMutex
	queue      QueueStarter
}

type Params struct {
	ExecutionID domainfoundation.AgentExecutionID
	Outcome     domainexecution.ExecutionOutcome
	FailureCode domainexecution.ExecutionFailureCode
}

func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions}, {"agents", config.Agents}, {"executions", config.Executions},
		{"queued work", config.QueuedWork}, {"controls", config.Controls}, {"events", config.Events},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("execution settlement service %s is required", required.name)
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
	return &Service{tx: config.Transactions, agents: config.Agents, executions: config.Executions, queuedWork: config.QueuedWork, controls: config.Controls, events: config.Events, clock: clock, ids: ids}, nil
}

// SetQueueStarter attaches FIFO progression after queue construction finishes.
func (s *Service) SetQueueStarter(starter QueueStarter) error {
	if starter == nil {
		return errors.New("queue starter is required")
	}
	s.queueMu.Lock()
	s.queue = starter
	s.queueMu.Unlock()
	return nil
}

// ConfirmExecutionStart accepts a fsynced execution-start receipt before
// SQLite clears the temporary source content.
func (s *Service) ConfirmExecutionStart(ctx context.Context, receipt sessionport.ExecutionStartReceipt) error {
	if ctx == nil {
		return errors.New("execution start receipt context is required")
	}
	if strings.TrimSpace(receipt.ExecutionID.String()) == "" || strings.TrimSpace(receipt.EntryID) == "" {
		return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := s.executions.Get(txCtx, receipt.ExecutionID)
		if err != nil {
			return err
		}
		if execution.RequestID != receipt.RequestID {
			return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
		}
		if execution.Status == domainexecution.ExecutionStarting {
			if err := execution.MarkRunning(s.clock.Now()); err != nil {
				return err
			}
		}
		if execution.Status != domainexecution.ExecutionRunning && execution.Status != domainexecution.ExecutionSettling {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		if execution.StartContent != "" {
			if err := execution.ClearStartContent(receipt.InputDigest); err != nil {
				return err
			}
		}
		return s.executions.Save(txCtx, execution)
	})
}

// SettleRuntimeExecution is the runtime callback after its JSONL settlement
// receipt is durable.
func (s *Service) SettleRuntimeExecution(ctx context.Context, executionID domainfoundation.AgentExecutionID, outcome domainexecution.ExecutionOutcome, failureCode domainexecution.ExecutionFailureCode) error {
	return s.Settle(ctx, Params{ExecutionID: executionID, Outcome: outcome, FailureCode: failureCode})
}

// Settle applies execution, Agent, queued work and control transitions in one transaction.
func (s *Service) Settle(ctx context.Context, params Params) error {
	if ctx == nil {
		return errors.New("settlement context is required")
	}
	if strings.TrimSpace(params.ExecutionID.String()) == "" || !knownOutcome(params.Outcome) {
		return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	var settledAgentID domainfoundation.AgentID
	var advanceQueue bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		execution, err := s.executions.Get(txCtx, params.ExecutionID)
		if err != nil {
			return err
		}
		if execution.Status == domainexecution.ExecutionSettled {
			return nil
		}
		at := s.clock.Now()
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
		if err := execution.Settle(params.Outcome, params.FailureCode, at); err != nil {
			return err
		}
		agent, err := s.agents.Get(txCtx, execution.AgentID)
		if err != nil {
			return err
		}
		if agent.CurrentExecutionID != execution.ID {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		if err := agent.Settle(params.Outcome, at); err != nil {
			return err
		}
		if execution.WorkItemID != "" {
			work, err := s.queuedWork.Get(txCtx, execution.WorkItemID)
			if err != nil {
				return err
			}
			if work.AgentID != agent.ID || work.ExecutionID != execution.ID {
				return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
			}
			if err := work.Settle(execution.ID, params.Outcome, params.FailureCode, at); err != nil {
				return err
			}
			if err := s.queuedWork.Save(txCtx, work); err != nil {
				return err
			}
			if err := s.appendEvent(txCtx, domainfoundation.EventQueuedWorkSettled, work.SessionID, work.AgentID, work.ID, execution.ID, params, at); err != nil {
				return err
			}
			advanceQueue = params.Outcome == domainexecution.ExecutionCompleted || params.Outcome == domainexecution.ExecutionYielded
		}
		controls, err := s.controls.ListOpenByAgent(txCtx, agent.ID, 100)
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
			if err := s.controls.Save(txCtx, *control); err != nil {
				return err
			}
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		eventType := domainfoundation.EventExecutionSettled
		switch params.Outcome {
		case domainexecution.ExecutionFailed:
			eventType = domainfoundation.EventAgentFailed
		case domainexecution.ExecutionInterrupted:
			eventType = domainfoundation.EventAgentInterrupted
		case domainexecution.ExecutionPaused:
			eventType = domainfoundation.EventAgentPaused
		}
		if err := s.appendEvent(txCtx, eventType, execution.SessionID, execution.AgentID, "", execution.ID, params, at); err != nil {
			return err
		}
		settledAgentID = agent.ID
		return s.agents.Save(txCtx, agent)
	})
	if err != nil || !advanceQueue {
		return err
	}
	s.queueMu.RLock()
	queue := s.queue
	s.queueMu.RUnlock()
	if queue != nil {
		_, _ = queue.StartNextQueuedWork(context.WithoutCancel(ctx), settledAgentID)
	}
	return nil
}

func (s *Service) appendEvent(ctx context.Context, eventType domainfoundation.DomainEventType, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, workID domainfoundation.WorkItemID, executionID domainfoundation.AgentExecutionID, params Params, at time.Time) error {
	event := domainfoundation.NewDomainEvent(eventType, at)
	event.ID = domainfoundation.EventID(s.ids.New("event"))
	event.SessionID, event.AgentID, event.WorkItemID, event.AgentExecutionID = sessionID, agentID, workID, executionID
	event.Payload = map[string]string{"outcome": string(params.Outcome), "failureCode": string(params.FailureCode)}
	return s.events.Append(ctx, event)
}

func knownOutcome(outcome domainexecution.ExecutionOutcome) bool {
	switch outcome {
	case domainexecution.ExecutionCompleted, domainexecution.ExecutionYielded, domainexecution.ExecutionPaused, domainexecution.ExecutionFailed, domainexecution.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
