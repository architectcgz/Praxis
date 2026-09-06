// Package queue owns durable independent work admission and FIFO execution start.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commandprotocol "praxis/internal/command"
	domainagent "praxis/internal/domain/agent"
	domaincommand "praxis/internal/domain/command"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/persistence"
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type Readiness interface{ Ready() bool }

type InputFactory interface {
	MaterializeExecutionInput(context.Context, domainagent.Agent, string, string, string) (domainexecution.ExecutionInputSnapshot, error)
}

type RuntimeActivator interface {
	TryActivate(context.Context, domainfoundation.AgentID, runtimecontract.ExecutionLifecycle) error
}

type Config struct {
	Transactions persistence.TxRunner
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	QueuedWork   persistence.QueuedWorkRepository
	Deliveries   persistence.ContextDeliveryRepository
	Receipts     persistence.CommandReceiptRepository
	Events       persistence.EventRepository
	Readiness    Readiness
	Inputs       InputFactory
	Activator    RuntimeActivator
	Lifecycle    runtimecontract.ExecutionLifecycle
	Clock        system.Clock
	IDs          system.IDGenerator
}

type Service struct {
	tx         persistence.TxRunner
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	queuedWork persistence.QueuedWorkRepository
	deliveries persistence.ContextDeliveryRepository
	receipts   persistence.CommandReceiptRepository
	events     persistence.EventRepository
	readiness  Readiness
	inputs     InputFactory
	activator  RuntimeActivator
	lifecycle  runtimecontract.ExecutionLifecycle
	clock      system.Clock
	ids        system.IDGenerator
}

type EnqueueParams struct {
	ID        domainfoundation.WorkItemID
	RequestID domainfoundation.RequestID
	AgentID   domainfoundation.AgentID
	Prompt    string
}

type EnqueueResult struct {
	Work            domainworkflow.QueuedWork
	ExistingWork    bool
	ActivationError string
}

type StartResult struct {
	Work            domainworkflow.QueuedWork
	Execution       domainexecution.AgentExecution
	Started         bool
	ActivationError string
}

func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions}, {"agents", config.Agents}, {"executions", config.Executions},
		{"queued work", config.QueuedWork}, {"deliveries", config.Deliveries}, {"command receipts", config.Receipts},
		{"events", config.Events}, {"readiness", config.Readiness}, {"execution input factory", config.Inputs},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("execution queue service %s is required", required.name)
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
	return &Service{tx: config.Transactions, agents: config.Agents, executions: config.Executions, queuedWork: config.QueuedWork, deliveries: config.Deliveries, receipts: config.Receipts, events: config.Events, readiness: config.Readiness, inputs: config.Inputs, activator: config.Activator, lifecycle: config.Lifecycle, clock: clock, ids: ids}, nil
}

func (s *Service) EnqueueWork(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue work context is required")
	}
	if !s.readiness.Ready() {
		return EnqueueResult{}, commandprotocol.NewError(commandprotocol.ErrorNotReady)
	}
	if strings.TrimSpace(params.ID.String()) == "" || strings.TrimSpace(params.RequestID.String()) == "" || strings.TrimSpace(params.AgentID.String()) == "" || strings.TrimSpace(params.Prompt) == "" {
		return EnqueueResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	result := EnqueueResult{}
	startEligible := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		digest := commandprotocol.ArgumentsDigest(struct {
			WorkID  domainfoundation.WorkItemID
			AgentID domainfoundation.AgentID
			Prompt  string
		}{params.ID, params.AgentID, strings.TrimSpace(params.Prompt)})
		receipt, found, err := commandprotocol.FindReceipt(txCtx, s.receipts, params.RequestID, "queue_work", digest)
		if err != nil {
			return err
		}
		if found {
			var value struct{ WorkID string }
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			work, err := s.queuedWork.Get(txCtx, domainfoundation.WorkItemID(value.WorkID))
			if err != nil {
				return err
			}
			result.Work, result.ExistingWork = work, true
			return nil
		}
		existing, err := s.queuedWork.Get(txCtx, params.ID)
		if err == nil {
			if existing.AgentID != params.AgentID || existing.Prompt != strings.TrimSpace(params.Prompt) {
				return commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
			}
			result.Work, result.ExistingWork = existing, true
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		sequence, err := s.queuedWork.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		input, err := s.inputs.MaterializeExecutionInput(txCtx, agent, "", "", "")
		if err != nil {
			return err
		}
		at := s.clock.Now()
		work, err := domainworkflow.NewQueuedWork(params.ID, agent.SessionID, agent.ID, sequence, params.Prompt, input, at)
		if err != nil {
			return err
		}
		if err := s.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventQueuedWorkCreated, work.SessionID, work.AgentID, work.ID, "", at); err != nil {
			return err
		}
		payload, _ := json.Marshal(struct{ WorkID string }{work.ID.String()})
		if err := s.receipts.Save(txCtx, domaincommand.CommandReceipt{RequestID: params.RequestID, Command: "queue_work", ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: at}); err != nil {
			return err
		}
		result.Work = work
		startEligible = startable(agent.State)
		return nil
	})
	if err != nil || result.ExistingWork || !startEligible {
		return result, err
	}
	started, err := s.StartNextQueuedWork(context.WithoutCancel(ctx), params.AgentID)
	if err != nil {
		result.ActivationError = err.Error()
	} else {
		result.ActivationError = started.ActivationError
	}
	return result, nil
}

func (s *Service) StartNextQueuedWork(ctx context.Context, agentID domainfoundation.AgentID) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("start queued work context is required")
	}
	if strings.TrimSpace(agentID.String()) == "" {
		return StartResult{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	result := StartResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if !startable(agent.State) {
			return nil
		}
		delivering, err := s.deliveries.HasDeliveringByTarget(txCtx, agent.ID)
		if err != nil {
			return err
		}
		if delivering {
			return nil
		}
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return nil
		}
		work, err := s.queuedWork.FindNextPendingByAgent(txCtx, agent.ID)
		if errors.Is(err, domainfoundation.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		at := s.clock.Now()
		execution, err := domainexecution.NewQueuedWorkExecution(domainfoundation.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, work.ID, work.Input, at)
		if err != nil {
			return err
		}
		if err := work.Start(execution.ID, at); err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := s.queuedWork.Save(txCtx, work); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventQueuedWorkStarted, work.SessionID, work.AgentID, work.ID, execution.ID, at); err != nil {
			return err
		}
		result.Work, result.Execution, result.Started = work, execution, true
		return nil
	})
	if err != nil || !result.Started || s.activator == nil || s.lifecycle == nil {
		return result, err
	}
	if err := s.activator.TryActivate(context.WithoutCancel(ctx), agentID, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func startable(state domainagent.AgentState) bool {
	return state == domainagent.AgentIdle || state == domainagent.AgentWaiting || state == domainagent.AgentFailed || state == domainagent.AgentClosed
}

func (s *Service) appendEvent(ctx context.Context, eventType domainfoundation.DomainEventType, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, workID domainfoundation.WorkItemID, executionID domainfoundation.AgentExecutionID, at time.Time) error {
	return s.events.Append(ctx, domainfoundation.DomainEvent{ID: domainfoundation.EventID(s.ids.New("event")), Type: eventType, SessionID: sessionID, AgentID: agentID, WorkItemID: workID, AgentExecutionID: executionID, OccurredAt: at})
}
