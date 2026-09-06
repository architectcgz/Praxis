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

type ContextDeliveryClaim struct {
	Delivery domainworkflow.ContextDelivery
	Claimed  bool
}

// ClaimContextDelivery blocks a concurrent input start while the JSONL
// artifact is being appended. It performs no transcript I/O itself.
func (o *AgentOrchestrator) ClaimContextDelivery(
	ctx context.Context,
	deliveryID domainfoundation.DeliveryID,
) (ContextDeliveryClaim, error) {
	if ctx == nil {
		return ContextDeliveryClaim{}, errors.New("context delivery claim context is required")
	}
	if strings.TrimSpace(deliveryID.String()) == "" {
		return ContextDeliveryClaim{}, commandError(CommandErrorInvalidRequest)
	}
	claim := ContextDeliveryClaim{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := o.deliveries.Get(txCtx, deliveryID)
		if err != nil {
			return err
		}
		claim.Delivery = delivery
		if delivery.Status == domainworkflow.ContextDeliveryDelivering {
			return nil
		}
		if delivery.Status != domainworkflow.ContextDeliveryPending {
			return nil
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domainagent.AgentIdle && agent.State != domainagent.AgentWaiting && agent.State != domainagent.AgentFailed &&
			agent.State != domainagent.AgentClosed {
			return nil
		}
		if err := delivery.Begin(o.clock.Now()); err != nil {
			return err
		}
		if err := o.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		claim.Delivery = delivery
		claim.Claimed = true
		return nil
	})
	if err != nil {
		return ContextDeliveryClaim{}, err
	}
	return claim, nil
}

type ContextDeliveryCompletionRequest struct {
	DeliveryID       domainfoundation.DeliveryID
	ArtifactEntryRef string
	RequestID        domainfoundation.RequestID
}

type ContextDeliveryCompletion struct {
	Delivery         domainworkflow.ContextDelivery
	Execution        domainexecution.AgentExecution
	ExistingDelivery bool
	ActivationError  string
}

// CompleteContextDelivery runs only after the target Agent JSONL has fsynced
// the artifact receipt. Delivery, eligible waits, Agent state, and the next
// context_delivery execution become one SQLite transaction.
func (o *AgentOrchestrator) CompleteContextDelivery(
	ctx context.Context,
	request ContextDeliveryCompletionRequest,
) (ContextDeliveryCompletion, error) {
	if ctx == nil {
		return ContextDeliveryCompletion{}, errors.New("context delivery completion context is required")
	}
	if strings.TrimSpace(request.DeliveryID.String()) == "" || strings.TrimSpace(request.ArtifactEntryRef) == "" || request.RequestID == "" {
		return ContextDeliveryCompletion{}, commandError(CommandErrorInvalidRequest)
	}
	digest := commandArgumentsDigest(struct {
		DeliveryID       domainfoundation.DeliveryID
		ArtifactEntryRef string
	}{request.DeliveryID, request.ArtifactEntryRef})
	result := ContextDeliveryCompletion{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		if receipt, found, err := commandReceipt(txCtx, o.commandReceipts, request.RequestID, "complete_context_delivery", digest); err != nil {
			return err
		} else if found {
			var value struct{ ExecutionID string }
			if err := json.Unmarshal(receipt.ResultPayload, &value); err != nil {
				return err
			}
			delivery, err := o.deliveries.Get(txCtx, request.DeliveryID)
			if err != nil {
				return err
			}
			execution, err := o.executions.Get(txCtx, domainfoundation.AgentExecutionID(value.ExecutionID))
			if err != nil {
				return err
			}
			result = ContextDeliveryCompletion{Delivery: delivery, Execution: execution, ExistingDelivery: true}
			return nil
		}
		delivery, err := o.deliveries.Get(txCtx, request.DeliveryID)
		if err != nil {
			return err
		}
		if delivery.Status == domainworkflow.ContextDeliveryDelivered {
			if delivery.ArtifactEntryRef != request.ArtifactEntryRef {
				return domainfoundation.ErrRequestConflict
			}
			result.Delivery = delivery
			result.ExistingDelivery = true
			return nil
		}
		if delivery.Status != domainworkflow.ContextDeliveryDelivering {
			return commandError(CommandErrorAgentUnavailable)
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domainagent.AgentIdle && agent.State != domainagent.AgentWaiting && agent.State != domainagent.AgentFailed &&
			agent.State != domainagent.AgentClosed {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.checkSessionCapacity(txCtx, agent.SessionID); err != nil {
			return err
		}
		at := o.clock.Now()
		if err := delivery.MarkDelivered(request.ArtifactEntryRef, at); err != nil {
			return err
		}
		waits, err := o.waits.ListUnresolvedByAgent(txCtx, agent.ID, 100)
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
				if err := o.waits.Save(txCtx, *wait); err != nil {
					return err
				}
			}
		}
		requestID := domainfoundation.RequestID("delivery:" + delivery.ID.String())
		input, err := o.MaterializeExecutionInput(txCtx, agent, "", "", "")
		if err != nil {
			return err
		}
		execution, err := domainexecution.NewAgentExecution(
			domainfoundation.AgentExecutionID(o.newID("execution")),
			agent.SessionID,
			agent.ID,
			requestID,
			domainexecution.ExecutionContextDelivery,
			"",
			input,
			at,
		)
		if err != nil {
			return err
		}
		if err := agent.Start(execution.ID, at); err != nil {
			return err
		}
		if err := o.deliveries.Save(txCtx, delivery); err != nil {
			return err
		}
		deliveryEvent := o.newEvent(domainfoundation.EventDeliveryDelivered, at)
		deliveryEvent.SessionID, deliveryEvent.AgentID, deliveryEvent.DeliveryID = delivery.SessionID, delivery.TargetAgentID, delivery.ID
		if err := o.appendEvent(txCtx, deliveryEvent); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		executionEvent := o.newEvent(domainfoundation.EventExecutionStarted, at)
		executionEvent.SessionID, executionEvent.AgentID, executionEvent.AgentExecutionID = execution.SessionID, execution.AgentID, execution.ID
		if err := o.appendEvent(txCtx, executionEvent); err != nil {
			return err
		}
		if o.commandReceipts != nil {
			payload, _ := json.Marshal(struct{ ExecutionID string }{execution.ID.String()})
			if err := o.commandReceipts.Save(txCtx, domaincommand.CommandReceipt{
				RequestID: request.RequestID, Command: "complete_context_delivery",
				ArgumentsDigest: digest, ResultPayload: payload, CreatedAt: at,
			}); err != nil {
				return err
			}
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Delivery = delivery
		result.Execution = execution
		return nil
	})
	if err != nil {
		return ContextDeliveryCompletion{}, err
	}
	lifecycle := o.executionLifecycle()
	if result.ExistingDelivery || o.activator == nil || lifecycle == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), result.Execution.AgentID, lifecycle); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func waitTargets(wait *domainworkflow.WaitCondition, targetID string) bool {
	for _, candidate := range wait.TargetIDs {
		if candidate == targetID {
			return true
		}
	}
	return false
}
