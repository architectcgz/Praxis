package orchestrate

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	coresession "praxis/internal/core/session"
)

// ContextArtifactResolver returns approved, structured content for one durable
// delivery. It must never load or return a source Agent's raw transcript.
type ContextArtifactResolver func(context.Context, domain.ContextDelivery) (coresession.ContextArtifact, error)

type DeliverySessionHeaderResolver func(
	context.Context,
	domain.ContextDelivery,
) (coresession.AgentSessionHeader, error)

type DeliveryCoordinatorConfig struct {
	Orchestrator    *AgentOrchestrator
	Sessions        AgentSessionResolver
	SessionHeader   DeliverySessionHeaderResolver
	ResolveArtifact ContextArtifactResolver
}

// DeliveryCoordinator bridges the SQLite claim with the target Agent's JSONL
// receipt. SQLite state is mutated only through AgentOrchestrator.
type DeliveryCoordinator struct {
	orchestrator    *AgentOrchestrator
	sessions        AgentSessionResolver
	sessionHeader   DeliverySessionHeaderResolver
	resolveArtifact ContextArtifactResolver
}

func NewDeliveryCoordinator(config DeliveryCoordinatorConfig) (*DeliveryCoordinator, error) {
	if config.Orchestrator == nil {
		return nil, errors.New("delivery coordinator orchestrator is required")
	}
	if config.Sessions == nil {
		return nil, errors.New("delivery coordinator session resolver is required")
	}
	if config.ResolveArtifact == nil {
		return nil, errors.New("delivery coordinator artifact resolver is required")
	}
	return &DeliveryCoordinator{
		orchestrator:    config.Orchestrator,
		sessions:        config.Sessions,
		sessionHeader:   config.SessionHeader,
		resolveArtifact: config.ResolveArtifact,
	}, nil
}

type DeliveryAttemptResult struct {
	Delivery        domain.ContextDelivery
	Claimed         bool
	Completed       bool
	Execution       domain.AgentExecution
	ActivationError string
}

// TryDeliver writes the artifact exactly once. Active, paused, interrupted,
// failed, and closed targets remain untouched because ClaimContextDelivery does
// not move their delivery out of pending.
func (c *DeliveryCoordinator) TryDeliver(
	ctx context.Context,
	deliveryID domain.DeliveryID,
	runtimeSnapshot domain.RuntimeExecutionSnapshot,
) (DeliveryAttemptResult, error) {
	if ctx == nil {
		return DeliveryAttemptResult{}, errors.New("delivery context is required")
	}
	claim, err := c.orchestrator.ClaimContextDelivery(ctx, deliveryID)
	if err != nil {
		return DeliveryAttemptResult{}, err
	}
	result := DeliveryAttemptResult{Delivery: claim.Delivery, Claimed: claim.Claimed}
	if claim.Delivery.Status != domain.ContextDeliveryDelivering {
		return result, nil
	}
	artifact, err := c.resolveArtifact(ctx, claim.Delivery)
	if err != nil {
		return result, fmt.Errorf("resolve context delivery artifact: %w", err)
	}
	artifact.DeliveryID = claim.Delivery.ID
	store, err := c.sessions(claim.Delivery.SessionID, claim.Delivery.TargetAgentID)
	if err != nil {
		return result, fmt.Errorf("resolve target agent session: %w", err)
	}
	if store == nil {
		return result, errors.New("resolve target agent session returned nil store")
	}
	defer func() { _ = store.Close(context.Background()) }()
	if c.sessionHeader != nil {
		header, err := c.sessionHeader(ctx, claim.Delivery)
		if err != nil {
			return result, fmt.Errorf("resolve target agent session header: %w", err)
		}
		if err := store.Initialize(ctx, header); err != nil {
			return result, fmt.Errorf("initialize target agent session: %w", err)
		}
	}
	receipt, err := store.AppendContextArtifact(ctx, artifact)
	if err != nil {
		return result, fmt.Errorf("append target context artifact: %w", err)
	}
	completion, err := c.orchestrator.CompleteContextDelivery(ctx, ContextDeliveryCompletionRequest{
		DeliveryID:       claim.Delivery.ID,
		ArtifactEntryRef: receipt.EntryID,
		RuntimeSnapshot:  runtimeSnapshot,
	})
	if err != nil {
		return result, err
	}
	result.Delivery = completion.Delivery
	result.Completed = true
	result.Execution = completion.Execution
	result.ActivationError = completion.ActivationError
	return result, nil
}
