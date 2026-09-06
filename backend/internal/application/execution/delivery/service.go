// Package delivery owns durable ContextDelivery claim and completion commands.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	corecommand "praxis/internal/core/command"
	domainagent "praxis/internal/core/domain/agent"
	domaincommand "praxis/internal/core/domain/command"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"
	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
	"praxis/internal/core/system"
)

type InputFactory interface {
	MaterializeExecutionInput(context.Context, domainagent.Agent, string, string, string) (domainexecution.ExecutionInputSnapshot, error)
}
type RuntimeActivator interface {
	TryActivate(context.Context, domainfoundation.AgentID, coreruntime.ExecutionLifecycle) error
}

type Config struct {
	Transactions    persistence.TxRunner
	Agents          persistence.AgentRepository
	Executions      persistence.AgentExecutionRepository
	Waits           persistence.WaitConditionRepository
	Deliveries      persistence.ContextDeliveryRepository
	CommandReceipts persistence.CommandReceiptRepository
	Events          persistence.EventRepository
	Inputs          InputFactory
	Activator       RuntimeActivator
	Lifecycle       coreruntime.ExecutionLifecycle
	Clock           system.Clock
	IDs             system.IDGenerator
}

type Service struct {
	tx         persistence.TxRunner
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	waits      persistence.WaitConditionRepository
	deliveries persistence.ContextDeliveryRepository
	receipts   persistence.CommandReceiptRepository
	events     persistence.EventRepository
	inputs     InputFactory
	activator  RuntimeActivator
	lifecycle  coreruntime.ExecutionLifecycle
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
		{"deliveries", config.Deliveries}, {"command receipts", config.CommandReceipts}, {"events", config.Events}, {"execution input factory", config.Inputs},
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
	return &Service{tx: config.Transactions, agents: config.Agents, executions: config.Executions, waits: config.Waits, deliveries: config.Deliveries, receipts: config.CommandReceipts, events: config.Events, inputs: config.Inputs, activator: config.Activator, lifecycle: config.Lifecycle, clock: clock, ids: ids}, nil
}

// ClaimContextDelivery exclusively claims a pending delivery before JSONL artifact I/O.
func (s *Service) ClaimContextDelivery(ctx context.Context, deliveryID domainfoundation.DeliveryID) (Claim, error) {
	if ctx == nil {
		return Claim{}, errors.New("context delivery claim context is required")
	}
	if strings.TrimSpace(deliveryID.String()) == "" {
		return Claim{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
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
		return Completion{}, corecommand.NewError(corecommand.ErrorInvalidRequest)
	}
	digest := corecommand.ArgumentsDigest(struct {
		DeliveryID       domainfoundation.DeliveryID
		ArtifactEntryRef string
	}{params.DeliveryID, params.ArtifactEntryRef})
	result := Completion{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		receipt, found, err := corecommand.FindReceipt(txCtx, s.receipts, params.RequestID, "complete_context_delivery", digest)
		if err != nil {
			return err
		}
		if found {
			var value struct{ ExecutionID string }
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			delivery, err := s.deliveries.Get(txCtx, params.DeliveryID)
			if err != nil {
				return err
			}
			execution, err := s.executions.Get(txCtx, domainfoundation.AgentExecutionID(value.ExecutionID))
			if err != nil {
				return err
			}
			result = Completion{Delivery: delivery, Execution: execution, ExistingDelivery: true}
			return nil
		}
		delivery, err := s.deliveries.Get(txCtx, params.DeliveryID)
		if err != nil {
			return err
		}
		if delivery.Status == domainworkflow.ContextDeliveryDelivered {
			if delivery.ArtifactEntryRef != params.ArtifactEntryRef {
				return domainfoundation.ErrRequestConflict
			}
			result.Delivery, result.ExistingDelivery = delivery, true
			return nil
		}
		if delivery.Status != domainworkflow.ContextDeliveryDelivering {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		agent, err := s.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if !startable(agent.State) {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		active, err := s.executions.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active >= 1 {
			return corecommand.NewError(corecommand.ErrorAgentUnavailable)
		}
		at := s.clock.Now()
		if err := delivery.MarkDelivered(params.ArtifactEntryRef, at); err != nil {
			return err
		}
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
		input, err := s.inputs.MaterializeExecutionInput(txCtx, agent, "", "", "")
		if err != nil {
			return err
		}
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(s.ids.New("execution")), agent.SessionID, agent.ID, domainfoundation.RequestID("delivery:"+delivery.ID.String()), domainexecution.ExecutionContextDelivery, "", input, at)
		if err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := s.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventDeliveryDelivered, delivery.SessionID, delivery.TargetAgentID, delivery.ID, "", nil, at); err != nil {
			return err
		}
		if err := s.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := s.appendEvent(txCtx, domainfoundation.EventExecutionStarted, execution.SessionID, execution.AgentID, "", execution.ID, nil, at); err != nil {
			return err
		}
		payload, _ := json.Marshal(struct{ ExecutionID string }{execution.ID.String()})
		if err := s.receipts.Save(txCtx, domaincommand.CommandReceipt{RequestID: params.RequestID, Command: "complete_context_delivery", ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: at}); err != nil {
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
func (s *Service) appendEvent(ctx context.Context, eventType domainfoundation.DomainEventType, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, deliveryID domainfoundation.DeliveryID, executionID domainfoundation.AgentExecutionID, payload map[string]string, at time.Time) error {
	event := domainfoundation.NewDomainEvent(eventType, at)
	event.ID = domainfoundation.EventID(s.ids.New("event"))
	event.SessionID, event.AgentID, event.DeliveryID, event.AgentExecutionID, event.Payload = sessionID, agentID, deliveryID, executionID, payload
	return s.events.Append(ctx, event)
}
