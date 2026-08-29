package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/core/domain"
)

type ContextDeliveryClaim struct {
	Delivery domain.ContextDelivery
	Claimed  bool
}

// ClaimContextDelivery blocks a concurrent input start while the JSONL
// artifact is being appended. It performs no transcript I/O itself.
func (o *AgentOrchestrator) ClaimContextDelivery(
	ctx context.Context,
	deliveryID domain.DeliveryID,
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
		if delivery.Status == domain.ContextDeliveryDelivering {
			return nil
		}
		if delivery.Status != domain.ContextDeliveryPending {
			return nil
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
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
	DeliveryID       domain.DeliveryID
	ArtifactEntryRef string
	RuntimeSnapshot  domain.RuntimeExecutionSnapshot
}

type ContextDeliveryCompletion struct {
	Delivery         domain.ContextDelivery
	Execution        domain.AgentExecution
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
	if strings.TrimSpace(request.DeliveryID.String()) == "" || strings.TrimSpace(request.ArtifactEntryRef) == "" {
		return ContextDeliveryCompletion{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return ContextDeliveryCompletion{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	result := ContextDeliveryCompletion{}
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		delivery, err := o.deliveries.Get(txCtx, request.DeliveryID)
		if err != nil {
			return err
		}
		if delivery.Status == domain.ContextDeliveryDelivered {
			if delivery.ArtifactEntryRef != request.ArtifactEntryRef {
				return domain.ErrRequestConflict
			}
			result.Delivery = delivery
			result.ExistingDelivery = true
			return nil
		}
		if delivery.Status != domain.ContextDeliveryDelivering {
			return commandError(CommandErrorAgentUnavailable)
		}
		agent, err := o.agents.Get(txCtx, delivery.TargetAgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentIdle && agent.State != domain.AgentWaiting && agent.State != domain.AgentFailed &&
			agent.State != domain.AgentClosed {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
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
		requestID := domain.RequestID("delivery:" + delivery.ID.String())
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			requestID,
			domain.ExecutionContextDelivery,
			"",
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: agent.GrantID,
				Runtime:           request.RuntimeSnapshot,
			},
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
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
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
	if result.ExistingDelivery || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), result.Execution.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func waitTargets(wait *domain.WaitCondition, targetID string) bool {
	for _, candidate := range wait.TargetIDs {
		if candidate == targetID {
			return true
		}
	}
	return false
}
