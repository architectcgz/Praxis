package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	domainagent "praxis/internal/core/domain/agent"
	domaincontext "praxis/internal/core/domain/context"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
)

type SendInputRequest struct {
	SessionID  domainfoundation.SessionID
	AgentID    domainfoundation.AgentID
	RequestID  domainfoundation.RequestID
	Content    string
	ProviderID string
	ModelID    string
	Reasoning  string
}

type SendInputResult struct {
	Execution       domainexecution.AgentExecution
	ExistingRequest bool
	ActivationError string
}

func (o *AgentOrchestrator) SendInput(ctx context.Context, request SendInputRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("send input context is required")
	}
	if !o.Ready() {
		return SendInputResult{}, commandError(CommandErrorNotReady)
	}
	if request.RequestID == "" || strings.TrimSpace(request.Content) == "" || (request.SessionID == "" && request.AgentID == "") {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.ModelID) == "" {
		return SendInputResult{}, commandError(CommandErrorModelNotConfigured)
	}
	if request.AgentID == "" {
		created, err := o.EnsurePrimaryAgent(ctx, request.SessionID, request.RequestID)
		if err != nil {
			return SendInputResult{}, err
		}
		request.AgentID = created.Agent.ID
	}
	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(existing, domainexecution.ExecutionUserInput, request.Content) {
				return domainfoundation.ErrRequestConflict
			}
			if !o.executionModelMatches(txCtx, existing, request.ProviderID, request.ModelID, request.Reasoning) {
				return domainfoundation.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		if agent.SessionID != request.SessionID && request.SessionID != "" {
			return commandError(CommandErrorInvalidRequest)
		}
		switch agent.State {
		case domainagent.AgentExecuting, domainagent.AgentPausing:
			return commandError(CommandErrorAgentExecuting)
		case domainagent.AgentIdle, domainagent.AgentWaiting, domainagent.AgentFailed, domainagent.AgentClosed:
		default:
			return commandError(CommandErrorAgentUnavailable)
		}
		if err := o.rejectDeliveringInput(txCtx, agent.ID); err != nil {
			return err
		}
		if err := o.checkSessionCapacity(txCtx, agent.SessionID); err != nil {
			return err
		}
		input, err := o.materializeExecutionInput(txCtx, agent, request.ProviderID, request.ModelID, request.Reasoning)
		if err != nil {
			return err
		}
		at := o.clock.Now().UTC()
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(o.newID("execution")), agent.SessionID, agent.ID, request.RequestID, domainexecution.ExecutionUserInput, request.Content, input, at)
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
		event := o.newEvent(domainfoundation.EventExecutionStarted, at)
		event.SessionID, event.AgentID, event.AgentExecutionID = execution.SessionID, execution.AgentID, execution.ID
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
		if errors.Is(err, domainfoundation.ErrRequestConflict) {
			existing, lookupErr := o.executions.FindByRequest(ctx, request.AgentID, request.RequestID)
			if lookupErr != nil {
				return SendInputResult{}, lookupErr
			}
			if !executionRequestMatches(existing, domainexecution.ExecutionUserInput, request.Content) || !o.executionModelMatches(ctx, existing, request.ProviderID, request.ModelID, request.Reasoning) {
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

type ResumeRequest struct {
	AgentID   domainfoundation.AgentID
	RequestID domainfoundation.RequestID
	Content   string
}

func (o *AgentOrchestrator) Resume(ctx context.Context, request ResumeRequest) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("resume context is required")
	}
	if !o.Ready() || request.AgentID == "" || request.RequestID == "" {
		return SendInputResult{}, commandError(CommandErrorInvalidRequest)
	}
	var result SendInputResult
	err := o.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := o.executions.FindByRequest(txCtx, request.AgentID, request.RequestID)
		if err == nil {
			if !executionRequestMatches(existing, domainexecution.ExecutionResume, request.Content) {
				return domainfoundation.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		agent, err := o.agents.Get(txCtx, request.AgentID)
		if err != nil {
			return err
		}
		if agent.State != domainagent.AgentPaused && agent.State != domainagent.AgentInterrupted {
			return commandError(CommandErrorAgentUnavailable)
		}
		model, err := o.models.ResolveModel(agent.Profile)
		if err != nil {
			return fmt.Errorf("resolve model: %w", err)
		}
		input, err := o.materializeExecutionInput(txCtx, agent, model.ProviderID, model.ModelID, model.Reasoning)
		if err != nil {
			return err
		}
		at := o.clock.Now().UTC()
		execution, err := domainexecution.NewAgentExecution(domainfoundation.AgentExecutionID(o.newID("execution")), agent.SessionID, agent.ID, request.RequestID, domainexecution.ExecutionResume, request.Content, input, at)
		if err != nil {
			return err
		}
		previous, err := o.executions.ListByAgent(txCtx, agent.ID, 1)
		if err != nil {
			return err
		}
		if len(previous) > 0 {
			execution.ParentExecutionID = previous[0].ID
		}
		if err := execution.Validate(); err != nil {
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
		event := o.newEvent(domainfoundation.EventExecutionStarted, at)
		event.SessionID, event.AgentID, event.AgentExecutionID = execution.SessionID, execution.AgentID, execution.ID
		if err := o.appendEvent(txCtx, event); err != nil {
			return err
		}
		result.Execution = execution
		return nil
	})
	if err != nil {
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

func (o *AgentOrchestrator) materializeExecutionInput(ctx context.Context, agent domainagent.Agent, providerID, modelID, reasoning string) (domainexecution.ExecutionInputSnapshot, error) {
	session, err := o.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	workspace, err := o.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	policy, err := o.policies.GetCurrent(ctx, agent.ID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	if policy.Revision != agent.SecurityPolicyRevision {
		return domainexecution.ExecutionInputSnapshot{}, domainfoundation.ErrRevisionConflict
	}
	selector, ok := o.models.(ModelSelectionResolver)
	if !ok {
		return domainexecution.ExecutionInputSnapshot{}, errors.New("model selection resolver is unavailable")
	}
	model, err := selector.ResolveModelSelection(providerID, modelID, reasoning)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, commandError(CommandErrorInvalidRequest)
	}
	contextRevision, err := o.contexts.CurrentRevision(ctx, agent.SessionID)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	if contextRevision == 0 {
		return domainexecution.ExecutionInputSnapshot{}, errors.New("session context has no initial revision")
	}
	entries := make([]domaincontext.SessionContextEntry, 0, contextRevision)
	for after := uint64(0); after < contextRevision; {
		page, err := o.contexts.List(ctx, agent.SessionID, after, 512)
		if err != nil {
			return domainexecution.ExecutionInputSnapshot{}, err
		}
		if len(page) == 0 {
			return domainexecution.ExecutionInputSnapshot{}, errors.New("session context revision sequence is incomplete")
		}
		for _, entry := range page {
			if entry.Revision != uint64(len(entries)+1) {
				return domainexecution.ExecutionInputSnapshot{}, errors.New("session context revision sequence is incomplete")
			}
			entries = append(entries, entry)
		}
		after = page[len(page)-1].Revision
	}
	entryRevisions := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		entryRevisions = append(entryRevisions, entry.Revision)
	}
	contextSummary := boundedContextSummary(entries)
	manifest, err := domaincontext.NewContextManifest(domainfoundation.ContextManifestID(o.newID("manifest")), contextSummary, nil, o.clock.Now().UTC())
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	security, err := o.security.Resolve(policy, domainsecurity.ExecutionRestrictions{}, workspace, model, manifest)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	runtimeSnapshot, err := domainexecution.NewRuntimeExecutionSnapshot(
		security.Sandbox.Mode,
		security.ApprovalRules[0].Mode,
		security.Fingerprint,
	)
	if err != nil {
		return domainexecution.ExecutionInputSnapshot{}, err
	}
	selection := domainexecution.ContextSelection{Revision: contextRevision, EntryRevisions: entryRevisions, Summary: contextSummary}
	return domainexecution.ExecutionInputSnapshot{ContextManifest: manifest, ContextSelection: selection, Security: security, Runtime: runtimeSnapshot}, nil
}

func boundedContextSummary(entries []domaincontext.SessionContextEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if content := strings.TrimSpace(entry.Content); content != "" {
			parts = append(parts, content)
		}
	}
	joined := strings.Join(parts, "\n\n")
	if len([]byte(joined)) <= domaincontext.MaxManifestSummaryBytes {
		return joined
	}
	var bounded strings.Builder
	for _, value := range joined {
		size := utf8.RuneLen(value)
		if size < 0 || bounded.Len()+size > domaincontext.MaxManifestSummaryBytes {
			break
		}
		bounded.WriteRune(value)
	}
	return strings.TrimSpace(bounded.String())
}

func (o *AgentOrchestrator) executionModelMatches(ctx context.Context, execution domainexecution.AgentExecution, providerID, modelID, reasoning string) bool {
	grant := execution.Input.Security.CapabilityGrant
	return (providerID == "" || grant.Model.ProviderID == providerID) && (modelID == "" || grant.Model.ModelID == modelID) && (reasoning == "" || grant.Model.Reasoning == reasoning)
}

func executionRequestMatches(execution domainexecution.AgentExecution, reason domainexecution.ExecutionReason, content string) bool {
	if execution.Reason != reason {
		return false
	}
	if execution.StartContent == "" {
		return content == ""
	}
	return execution.StartContent == content
}

func containsTool(tools []domainsecurity.ToolName, target domainsecurity.ToolName) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}
