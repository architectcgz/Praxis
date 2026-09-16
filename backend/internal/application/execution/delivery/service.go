// Package delivery owns durable ContextDelivery claim and completion commands.
package delivery

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
	runtimecontract "praxis/internal/runtime"
	"praxis/internal/system"
)

type InputFactory interface {
	MaterializeExecutionInput(context.Context, domainagent.Agent, string, string, string, []string) (domainexecution.ExecutionInputSnapshot, error)
}
type RuntimeActivator interface {
	TryActivate(context.Context, domainfoundation.AgentID, runtimecontract.ExecutionLifecycle) error
}

type Config struct {
	Transactions persistence.TxRunner
	Agents       persistence.AgentRepository
	Executions   persistence.AgentExecutionRepository
	Waits        persistence.WaitConditionRepository
	Deliveries   persistence.ContextDeliveryRepository
	Events       persistence.EventRepository
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
	waits      persistence.WaitConditionRepository
	deliveries persistence.ContextDeliveryRepository
	events     persistence.EventRepository
	inputs     InputFactory
	activator  RuntimeActivator
	lifecycle  runtimecontract.ExecutionLifecycle
	clock      system.Clock
	ids        system.IDGenerator
}

type Claim struct {
	Delivery domainworkflow.ContextDelivery
	Claimed  bool
}
type CompleteParams struct {
	DeliveryID       domainfoundation.DeliveryID
	ArtifactEntryRef string
	RequestID        domainfoundation.RequestID
}
type Completion struct {
	Delivery         domainworkflow.ContextDelivery
	Execution        domainexecution.AgentExecution
	ExistingDelivery bool
	ActivationError  string
}

func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{"transactions", config.Transactions}, {"agents", config.Agents}, {"executions", config.Executions}, {"waits", config.Waits},
		{"deliveries", config.Deliveries}, {"events", config.Events}, {"execution input factory", config.Inputs},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("execution delivery service %s is required", required.name)
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
	return &Service{tx: config.Transactions, agents: config.Agents, executions: config.Executions, waits: config.Waits, deliveries: config.Deliveries, events: config.Events, inputs: config.Inputs, activator: config.Activator, lifecycle: config.Lifecycle, clock: clock, ids: ids}, nil
}

// ClaimContextDelivery exclusively claims a pending delivery before JSONL artifact I/O.
func (s *Service) ClaimContextDelivery(ctx context.Context, deliveryID domainfoundation.DeliveryID) (Claim, error) {
	if ctx == nil {
		return Claim{}, errors.New("context delivery claim context is required")
	}
	if strings.TrimSpace(deliveryID.String()) == "" {
		return Claim{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	claim := Claim{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := s.deliveries.Get(txCtx, deliveryID)
		if err != nil {
			return err
		}
		claim.Delivery = delivery
		if delivery.Status == domainworkflow.ContextDeliveryDelivering || delivery.Status != domainworkflow.ContextDeliveryPending {
			return nil
		}
		agent, err := s.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if !startable(agent.State) {
			return nil
		}
		if err := delivery.Begin(s.clock.Now()); err != nil {
			return err
		}
		if err := s.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		claim.Delivery, claim.Claimed = delivery, true
		return nil
	})
	if err != nil {
		return Claim{}, err
	}
	return claim, nil
}

// CompleteContextDelivery records a fsynced artifact receipt and creates its execution atomically.
func (s *Service) CompleteContextDelivery(ctx context.Context, params CompleteParams) (Completion, error) {
	if ctx == nil {
		return Completion{}, errors.New("context delivery completion context is required")
	}
	if strings.TrimSpace(params.DeliveryID.String()) == "" || strings.TrimSpace(params.ArtifactEntryRef) == "" || params.RequestID == "" {
		return Completion{}, commandprotocol.NewError(commandprotocol.ErrorInvalidRequest)
	}
	result := Completion{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := s.deliveries.Get(txCtx, params.DeliveryID)
		if err != nil {
			return err
		}
		if delivery.Status == domainworkflow.ContextDeliveryDelivered {
			if delivery.ArtifactEntryRef != params.ArtifactEntryRef {
				return domainfoundation.ErrRequestConflict
			}
			execution, err := s.executions.Get(txCtx, delivery.ResultExecutionID)
			if err != nil {
				return err
			}
			result = Completion{Delivery: delivery, Execution: execution, ExistingDelivery: true}
			return nil
		}
		if delivery.Status != domainworkflow.ContextDeliveryDelivering {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		agent, err := s.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if !startable(agent.State) {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return commandprotocol.NewError(commandprotocol.ErrorAgentUnavailable)
		}
		at := s.clock.Now()
		waits, err := s.waits.ListUnresolvedByAgent(txCtx, agent.ID, 100)
		if err != nil {
			return err
		}
		for index := range waits {
			wait := &waits[index]
			if !waitTargets(wait, delivery.ID.String()) {
				continue
			}
			before := len(wait.ResolvedTargetIDs)
			if _, err := wait.ResolveTarget(delivery.ID.String(), at); err != nil {
				return err
			}
			if len(wait.ResolvedTargetIDs) != before {
				if err := s.waits.Save(txCtx, *wait); err != nil {
					return err
				}
			}
		}
		input, err := s.inputs.MaterializeExecutionInput(txCtx, agent, "", "", "", []string{params.ArtifactEntryRef})
		if err != nil {
			return err
		}
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, domainfoundation.RequestID("delivery:"+delivery.ID.String()), domainexecution.ExecutionContextDelivery, "", input, at)
		if err != nil {
			return err
		}
		if err := delivery.MarkDelivered(params.ArtifactEntryRef, execution.ID, at); err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := s.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		delivered := domainfoundation.NewDomainEvent(domainfoundation.EventDeliveryDelivered, at)
		delivered.ID = domainfoundation.EventID(s.ids.New("event"))
		delivered.SessionID, delivered.TargetAgentID, delivered.DeliveryID = delivery.SessionID, delivery.TargetAgentID, delivery.ID
		if err := s.events.Append(txCtx, delivered); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		started := domainfoundation.NewDomainEvent(domainfoundation.EventExecutionStarted, at)
		started.ID = domainfoundation.EventID(s.ids.New("event"))
		started.SessionID, started.AgentID, started.AgentExecutionID = execution.SessionID, execution.AgentID, execution.ID
		if err := s.events.Append(txCtx, started); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Delivery, result.Execution = delivery, execution
		return nil
	})
	if err != nil {
		return Completion{}, err
	}
	if result.ExistingDelivery || s.activator == nil || s.lifecycle == nil {
		return result, nil
	}
	if err := s.activator.TryActivate(context.WithoutCancel(ctx), result.Execution.AgentID, s.lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func startable(state domainagent.AgentState) bool {
	return state == domainagent.AgentIdle || state == domainagent.AgentWaiting || state == domainagent.AgentFailed || state == domainagent.AgentClosed
}
func waitTargets(wait *domainworkflow.WaitCondition, targetID string) bool {
	for _, candidate := range wait.TargetIDs {
		if candidate == targetID {
			return true
		}
	}
	return false
}
