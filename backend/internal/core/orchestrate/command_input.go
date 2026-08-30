package orchestrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/core/domain"
)

type SendInputRequest struct {
	SessionID       domain.SessionID
	AgentID         domain.AgentID
	RequestID       domain.RequestID
	Content         string
	ProviderID      string
	ModelID         string
	Reasoning       string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

type SendInputResult struct {
	Execution       domain.AgentExecution
	ExistingRequest bool
	ActivationError string
}

// SendInput either returns the prior execution for RequestID or atomically
// creates one starting execution. Active agents never retain ordinary input.
func (o *AgentOrchestrator) SendInput(ctx context.Context, request SendInputRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("send input context is required")
	}
	if !o.Ready() {
		return SendInputResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.SessionID.String()) == "" && strings.TrimSpace(request.AgentID.String()) == "" ||
		strings.TrimSpace(request.RequestID.String()) == "" || strings.TrimSpace(request.Content) == "" {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return SendInputResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	if request.AgentID == "" {
		created, err := o.EnsurePrimaryAgent(ctx, request.SessionID)
		if err != nil {
			return SendInputResult{}, err
		}
		request.AgentID = created.Agent.ID
	}

	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(
				existing,
				domain.ExecutionUserInput,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return domain.ErrRequestConflict
			}
			matches, matchErr := o.executionModelMatches(txCtx, existing, request.ProviderID, request.ModelID, request.Reasoning)
			if matchErr != nil {
				return matchErr
			}
			if !matches {
				return domain.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		switch agent.State {
		case domain.AgentExecuting, domain.AgentPausing:
			return commandError(CommandErrorAgentExecuting)
		case domain.AgentIdle, domain.AgentWaiting, domain.AgentFailed, domain.AgentClosed:
		default:
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			return err
		}
		grantID, err := o.selectedGrant(txCtx, agent, request.ProviderID, request.ModelID, request.Reasoning)
		if err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			request.RequestID,
			domain.ExecutionUserInput,
			request.Content,
			domain.ExecutionInputSnapshot{
				TaskPacketID:      agent.TaskPacketID,
				ContextManifestID: agent.ContextManifestID,
				CapabilityGrantID: grantID,
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
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrRequestConflict) {
			existing, lookupErr := o.executions.FindByRequest(ctx, request.AgentID, request.RequestID)
			if lookupErr != nil {
				return SendInputResult{}, lookupErr
			}
			if !executionRequestMatches(
				existing,
				domain.ExecutionUserInput,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return SendInputResult{}, err
			}
			matches, matchErr := o.executionModelMatches(
				ctx,
				existing,
				request.ProviderID,
				request.ModelID,
				request.Reasoning,
			)
			if matchErr != nil {
				return SendInputResult{}, matchErr
			}
			if !matches {
				return SendInputResult{}, err
			}
			return SendInputResult{Execution: existing, ExistingRequest: true}, nil
		}
		return SendInputResult{}, err
	}
	if result.ExistingRequest || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), request.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func (o *AgentOrchestrator) selectedGrant(
	ctx context.Context,
	agent domain.Agent,
	providerID string,
	modelID string,
	reasoning string,
) (domain.CapabilityGrantID, error) {
	base, err := o.grants.Get(ctx, agent.GrantID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(providerID) == "" && strings.TrimSpace(modelID) == "" && strings.TrimSpace(reasoning) == "" {
		return base.ID, nil
	}
	selector, ok := o.models.(ModelSelectionResolver)
	if !ok {
		return "", commandError(CommandErrorInvalidRequest)
	}
	modelID = strings.TrimSpace(modelID)
	if providerID == "" {
		providerID = base.Model.ProviderID
	}
	if modelID == "" {
		modelID = base.Model.ModelID
	}
	model, err := selector.ResolveModelSelection(providerID, modelID, strings.TrimSpace(reasoning))
	if err != nil {
		return "", commandError(CommandErrorInvalidRequest)
	}
	if model == base.Model {
		return base.ID, nil
	}
	grant := base.Snapshot()
	grant.ID = domain.NewCapabilityGrantID()
	grant.Model = model
	if err := grant.Validate(); err != nil {
		return "", err
	}
	if err := o.grants.Save(ctx, grant); err != nil {
		return "", err
	}
	return grant.ID, nil
}

func (o *AgentOrchestrator) executionModelMatches(
	ctx context.Context,
	execution domain.AgentExecution,
	providerID string,
	modelID string,
	reasoning string,
) (bool, error) {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	reasoning = strings.TrimSpace(reasoning)
	if providerID == "" && modelID == "" && reasoning == "" {
		return true, nil
	}
	grant, err := o.grants.Get(ctx, execution.Input.CapabilityGrantID)
	if err != nil {
		return false, err
	}
	if providerID != "" && grant.Model.ProviderID != providerID {
		return false, nil
	}
	if modelID != "" && grant.Model.ModelID != modelID {
		return false, nil
	}
	if reasoning != "" && grant.Model.Reasoning != reasoning {
		return false, nil
	}
	return true, nil
}

type ResumeRequest struct {
	AgentID         domain.AgentID
	RequestID       domain.RequestID
	Content         string
	RuntimeSnapshot domain.RuntimeExecutionSnapshot
}

// Resume creates a fresh execution with isolated runtime channels, model
// streams, tool state, and execution identity.
func (o *AgentOrchestrator) Resume(ctx context.Context, request ResumeRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("resume context is required")
	}
	if !o.Ready() {
		return SendInputResult{}, commandError(CommandErrorNotReady)
	}
	if strings.TrimSpace(request.AgentID.String()) == "" || strings.TrimSpace(request.RequestID.String()) == "" {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	if err := request.RuntimeSnapshot.Validate(); err != nil {
		return SendInputResult{}, fmt.Errorf("%w: %v", commandError(CommandErrorInvalidRequest), err)
	}
	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(
				existing,
				domain.ExecutionResume,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return domain.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		if agent.State != domain.AgentPaused && agent.State != domain.AgentInterrupted {
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			return err
		}
		if err := o.checkGroupCapacity(txCtx, agent.GroupID); err != nil {
			return err
		}
		at := o.clock.Now()
		execution, err := domain.NewAgentExecution(
			domain.NewAgentExecutionID(),
			agent.SessionID,
			agent.ID,
			request.RequestID,
			domain.ExecutionResume,
			request.Content,
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
		if err := agent.Resume(execution.ID, at); err != nil {
			return err
		}
		if err := o.executions.Save(txCtx, execution); err != nil {
			return err
		}
		if err := o.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrRequestConflict) {
			existing, lookupErr := o.executions.FindByRequest(ctx, request.AgentID, request.RequestID)
			if lookupErr != nil {
				return SendInputResult{}, lookupErr
			}
			if !executionRequestMatches(
				existing,
				domain.ExecutionResume,
				request.Content,
				request.RuntimeSnapshot,
			) {
				return SendInputResult{}, err
			}
			return SendInputResult{Execution: existing, ExistingRequest: true}, nil
		}
		return SendInputResult{}, err
	}
	if result.ExistingRequest || o.activator == nil {
		return result, nil
	}
	if err := o.activator.TryActivate(context.WithoutCancel(ctx), request.AgentID, o); err != nil {
		result.ActivationError = err.Error()
	}
	return result, nil
}

func executionRequestMatches(
	execution domain.AgentExecution,
	reason domain.ExecutionReason,
	content string,
	runtime domain.RuntimeExecutionSnapshot,
) bool {
	if execution.Reason != reason || execution.Input.Runtime != runtime {
		return false
	}
	if reason != domain.ExecutionUserInput && reason != domain.ExecutionResume {
		return true
	}
	if content == "" && execution.StartContent == "" && execution.StartContentDigest == "" {
		return true
	}
	if execution.StartContent != "" {
		return execution.StartContent == content
	}
	if execution.StartContentDigest == "" {
		return false
	}
	digest := sha256.Sum256([]byte(content))
	return execution.StartContentDigest == hex.EncodeToString(digest[:])
}
