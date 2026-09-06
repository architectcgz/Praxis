package orchestration

import (
	"context"
	"errors"
	"fmt"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainworkflow "praxis/internal/core/domain/workflow"

	coresession "praxis/internal/core/session"
)

// ContextArtifactResolver returns approved, structured content for one durable
// delivery. It must never load or return a source Agent's raw transcript.
type ContextArtifactResolver func(context.Context, domainworkflow.ContextDelivery) (coresession.ContextArtifact, error)

type DeliverySessionHeaderResolver func(
	context.Context,
	domainworkflow.ContextDelivery,
) (coresession.AgentSessionHeader, error)

// ContextDeliveryClaim records the durable state after a coordinator claims
// one pending delivery for JSONL artifact append.
type ContextDeliveryClaim struct {
	Delivery domainworkflow.ContextDelivery
	Claimed  bool
}

// ContextDeliveryCompletionRequest acknowledges the durable JSONL artifact.
type ContextDeliveryCompletionRequest struct {
	DeliveryID       domainfoundation.DeliveryID
	ArtifactEntryRef string
	RequestID        domainfoundation.RequestID
}

// ContextDeliveryCompletion contains the state produced by delivery completion.
type ContextDeliveryCompletion struct {
	Delivery         domainworkflow.ContextDelivery
	Execution        domainexecution.AgentExecution
	ExistingDelivery bool
	ActivationError  string
}

// DeliveryCommands is the durable application boundary used by delivery
// coordination. The coordinator never receives repositories directly.
type DeliveryCommands interface {
	ClaimContextDelivery(context.Context, domainfoundation.DeliveryID) (ContextDeliveryClaim, error)
	CompleteContextDelivery(context.Context, ContextDeliveryCompletionRequest) (ContextDeliveryCompletion, error)
}

type DeliveryCoordinatorConfig struct {
	Commands        DeliveryCommands
	Sessions        AgentSessionResolver
	SessionHeader   DeliverySessionHeaderResolver
	ResolveArtifact ContextArtifactResolver
}

// DeliveryCoordinator bridges durable application commands with the target
// Agent's JSONL receipt.
type DeliveryCoordinator struct {
	commands        DeliveryCommands
	sessions        AgentSessionResolver
	sessionHeader   DeliverySessionHeaderResolver
	resolveArtifact ContextArtifactResolver
}

func NewDeliveryCoordinator(config DeliveryCoordinatorConfig) (*DeliveryCoordinator, error) {
	if config.Commands == nil {
		return nil, errors.New("delivery coordinator commands are required")
	}
	if config.Sessions == nil {
		return nil, errors.New("delivery coordinator session resolver is required")
	}
	if config.ResolveArtifact == nil {
		return nil, errors.New("delivery coordinator artifact resolver is required")
	}
	return &DeliveryCoordinator{
		commands:        config.Commands,
		sessions:        config.Sessions,
		sessionHeader:   config.SessionHeader,
		resolveArtifact: config.ResolveArtifact,
	}, nil
}

type DeliveryAttemptResult struct {
	Delivery        domainworkflow.ContextDelivery
	Claimed         bool
	Completed       bool
	Execution       domainexecution.AgentExecution
	ActivationError string
}

// TryDeliver writes the artifact exactly once. Active, paused, interrupted,
// failed, and closed targets remain untouched because ClaimContextDelivery does
// not move their delivery out of pending.
func (c *DeliveryCoordinator) TryDeliver(
	ctx context.Context,
	deliveryID domainfoundation.DeliveryID,
) (DeliveryAttemptResult, error) {
	if ctx == nil {
		return DeliveryAttemptResult{}, errors.New("delivery context is required")
	}
	claim, err := c.commands.ClaimContextDelivery(ctx, deliveryID)
	if err != nil {
		return DeliveryAttemptResult{}, err
	}
	result := DeliveryAttemptResult{Delivery: claim.Delivery, Claimed: claim.Claimed}
	if claim.Delivery.Status != domainworkflow.ContextDeliveryDelivering {
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
	completion, err := c.commands.CompleteContextDelivery(ctx, ContextDeliveryCompletionRequest{
		DeliveryID:       claim.Delivery.ID,
		ArtifactEntryRef: receipt.EntryID,
		RequestID:        domainfoundation.RequestID("delivery:" + claim.Delivery.ID.String()),
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
