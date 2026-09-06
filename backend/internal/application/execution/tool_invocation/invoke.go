package toolinvocation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commandprotocol "praxis/internal/command"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
)

const settlementTimeout = 5 * time.Second

type invocationIdentity struct {
	Name      domainsecurity.ToolName
	Arguments json.RawMessage
}

// Invoke validates the runtime context, durably admits the call, and executes it at most once.
func (s *Service) Invoke(
	ctx context.Context,
	call runtimecontract.ToolCall,
	invocationContext runtimecontract.ToolInvocationContext,
) (runtimecontract.ToolResult, error) {
	if ctx == nil {
		return runtimecontract.ToolResult{}, errors.New("tool invocation context is required")
	}
	call = call.Snapshot()
	if strings.TrimSpace(call.ID) == "" || !call.Name.Valid() || len(call.Input) == 0 || !json.Valid(call.Input) {
		return toolError("invalid_tool_call", "The tool call is invalid."), nil
	}
	security, err := s.loadExecutionContext(ctx, invocationContext)
	if err != nil {
		return runtimecontract.ToolResult{}, err
	}
	if _, ok := s.catalog.Definition(call.Name); !ok {
		return toolError("invalid_tool_call", "The requested tool is not available."), nil
	}
	authorized, err := s.catalog.Normalize(call, invocationContext)
	if err != nil {
		return toolError("invalid_tool_call", "The tool arguments are invalid."), nil
	}
	authorized = authorized.Snapshot()
	if authorized.Name != call.Name || len(authorized.NormalizedArguments) == 0 ||
		!json.Valid(authorized.NormalizedArguments) {
		return runtimecontract.ToolResult{}, errors.New("tool catalog returned invalid normalized arguments")
	}
	authorized.ReadScopes = append([]string(nil), security.CapabilityGrant.ReadScopes...)
	digest := commandprotocol.ArgumentsDigest(invocationIdentity{
		Name: call.Name, Arguments: authorized.NormalizedArguments,
	})
	if digest == "" {
		return runtimecontract.ToolResult{}, errors.New("tool invocation arguments could not be fingerprinted")
	}
	invocation, created, err := s.findOrCreate(ctx, call, invocationContext, authorized, digest)
	if err != nil {
		if errors.Is(err, domainfoundation.ErrRequestConflict) {
			return toolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
		}
		return runtimecontract.ToolResult{}, err
	}
	if !created {
		if result, done := settledResult(invocation); done {
			return result, nil
		}
	}
	if invocation.Status == domainexecution.ToolInvocationRequested {
		invocation, err = s.admit(ctx, invocation, security, authorized)
		if err != nil {
			return runtimecontract.ToolResult{}, err
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
	}
	invocation, execute, err := s.start(ctx, invocation.ID)
	if err != nil {
		return runtimecontract.ToolResult{}, err
	}
	if !execute {
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		return toolError(
			string(domainexecution.ToolFailureResultUnknown),
			"The tool result is unavailable and the call was not replayed.",
		), nil
	}
	result, executeErr := s.executor.Execute(ctx, authorized)
	if executeErr != nil {
		if ctx.Err() != nil {
			interrupted := toolError(string(domainexecution.ToolFailureInterrupted), "The tool call was interrupted.")
			if err := s.interrupt(ctx, invocation.ID, interrupted); err != nil {
				return runtimecontract.ToolResult{}, err
			}
			return interrupted, ctx.Err()
		}
		failed := toolError(string(domainexecution.ToolFailureExecutionFailed), "The directory could not be listed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return runtimecontract.ToolResult{}, err
		}
		return failed, nil
	}
	if result.ErrorClass != "" || result.SideEffect {
		failed := toolError(string(domainexecution.ToolFailureExecutionFailed), "The directory could not be listed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return runtimecontract.ToolResult{}, err
		}
		return failed, nil
	}
	if result.Content == "" {
		result.Content = result.Output
	}
	result.Output = ""
	result.ErrorClass = ""
	result.SideEffect = false
	if err := s.succeed(ctx, invocation.ID, result); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	return result, nil
}

func (s *Service) loadExecutionContext(
	ctx context.Context,
	provided runtimecontract.ToolInvocationContext,
) (domainsecurity.ExecutionSecuritySnapshot, error) {
	if provided.ExecutionID == "" || provided.SessionID == "" || provided.AgentID == "" {
		return domainsecurity.ExecutionSecuritySnapshot{}, errors.New("tool invocation ownership is required")
	}
	execution, err := s.executions.Get(ctx, provided.ExecutionID)
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	if !execution.Active() || execution.SessionID != provided.SessionID || execution.AgentID != provided.AgentID {
		return domainsecurity.ExecutionSecuritySnapshot{}, errors.New(
			"tool invocation execution is not active or ownership does not match",
		)
	}
	security, err := s.security.Get(ctx, provided.ExecutionID)
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	if security.Fingerprint != execution.Input.Security.Fingerprint ||
		security.Fingerprint != provided.Execution.Revision ||
		provided.Execution != execution.Input.Runtime ||
		security.CapabilityGrant.ID != provided.Grant.ID ||
		commandprotocol.ArgumentsDigest(security.CapabilityGrant) != commandprotocol.ArgumentsDigest(provided.Grant) {
		return domainsecurity.ExecutionSecuritySnapshot{}, errors.New(
			"tool invocation security snapshot does not match the active execution",
		)
	}
	return security, nil
}

func (s *Service) findOrCreate(
	ctx context.Context,
	call runtimecontract.ToolCall,
	provided runtimecontract.ToolInvocationContext,
	authorized runtimecontract.AuthorizedToolCall,
	digest string,
) (domainexecution.ToolInvocation, bool, error) {
	var invocation domainexecution.ToolInvocation
	created := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.invocations.FindByExecutionCall(txCtx, provided.ExecutionID, call.ID)
		if err == nil {
			if existing.Name != call.Name || existing.ArgumentsDigest != digest {
				return domainfoundation.ErrRequestConflict
			}
			invocation = existing
			return nil
		}
		if !errors.Is(err, domainfoundation.ErrNotFound) {
			return err
		}
		invocation, err = domainexecution.NewToolInvocation(
			domainfoundation.ToolInvocationID(s.ids.New("toolinvocation")), provided.ExecutionID,
			provided.SessionID, provided.AgentID, call.ID, call.Name,
			authorized.NormalizedArguments, digest, s.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := s.invocations.Save(txCtx, invocation); err != nil {
			return err
		}
		created = true
		return nil
	})
	return invocation, created, err
}

func (s *Service) admit(
	ctx context.Context,
	invocation domainexecution.ToolInvocation,
	security domainsecurity.ExecutionSecuritySnapshot,
	call runtimecontract.AuthorizedToolCall,
) (domainexecution.ToolInvocation, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status != domainexecution.ToolInvocationRequested {
			invocation = current
			return nil
		}
		grant := security.CapabilityGrant
		allowed := grant.AllowsTool(current.Name) && !call.RequiresWrite && !call.RequiresNetwork
		if call.Path != "" {
			allowed = allowed && grant.AllowsReadPath(call.Path)
		}
		if !allowed {
			denied := domainexecution.ToolInvocationResult{
				InlineContent: toolErrorContent(
					string(domainexecution.ToolFailureNotAllowed),
					"The directory is outside the authorized read scope.",
				),
				ErrorCode: domainexecution.ToolFailureNotAllowed,
			}
			if err := current.Deny(denied, s.clock.Now()); err != nil {
				return err
			}
		} else {
			approval, err := domainsecurity.NewPolicyApproval(security.Fingerprint, s.clock.Now())
			if err != nil {
				return err
			}
			if err := current.Approve(approval); err != nil {
				return err
			}
		}
		if err := s.invocations.Save(txCtx, current); err != nil {
			return err
		}
		invocation = current
		return nil
	})
	return invocation, err
}

func (s *Service) start(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
) (domainexecution.ToolInvocation, bool, error) {
	var invocation domainexecution.ToolInvocation
	execute := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status == domainexecution.ToolInvocationApproved {
			if err := current.Start(s.clock.Now()); err != nil {
				return err
			}
			if err := s.invocations.Save(txCtx, current); err != nil {
				return err
			}
			execute = true
		}
		invocation = current
		return nil
	})
	return invocation, execute, err
}

func (s *Service) succeed(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
	result runtimecontract.ToolResult,
) error {
	domainResult := domainexecution.ToolInvocationResult{
		InlineContent: result.Content, SideEffect: result.SideEffect, Truncated: result.Truncated,
	}
	return s.settle(ctx, id, func(invocation *domainexecution.ToolInvocation) error {
		return invocation.Succeed(domainResult, s.clock.Now())
	})
}

func (s *Service) fail(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
	result runtimecontract.ToolResult,
) error {
	domainResult := domainexecution.ToolInvocationResult{
		InlineContent: result.Content, ErrorCode: domainexecution.ToolFailureExecutionFailed,
	}
	return s.settle(ctx, id, func(invocation *domainexecution.ToolInvocation) error {
		return invocation.Fail(domainResult, domainexecution.ToolFailureExecutionFailed, s.clock.Now())
	})
}

func (s *Service) interrupt(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
	result runtimecontract.ToolResult,
) error {
	domainResult := domainexecution.ToolInvocationResult{
		InlineContent: result.Content, ErrorCode: domainexecution.ToolFailureInterrupted,
	}
	return s.settle(ctx, id, func(invocation *domainexecution.ToolInvocation) error {
		return invocation.Interrupt(domainResult, s.clock.Now())
	})
}

func (s *Service) settle(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
	transition func(*domainexecution.ToolInvocation) error,
) error {
	settlementContext := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		settlementContext, cancel = context.WithTimeout(context.WithoutCancel(ctx), settlementTimeout)
	}
	defer cancel()
	return s.tx.InTx(settlementContext, func(txCtx context.Context) error {
		invocation, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if invocation.Status.Terminal() {
			return nil
		}
		if err := transition(&invocation); err != nil {
			return err
		}
		return s.invocations.Save(txCtx, invocation)
	})
}

func settledResult(invocation domainexecution.ToolInvocation) (runtimecontract.ToolResult, bool) {
	if !invocation.Status.Terminal() {
		return runtimecontract.ToolResult{}, false
	}
	return runtimecontract.ToolResult{
		Content: invocation.Result.InlineContent, ErrorClass: string(invocation.Result.ErrorCode),
		SideEffect: invocation.Result.SideEffect, Truncated: invocation.Result.Truncated,
	}, true
}

func toolError(code, message string) runtimecontract.ToolResult {
	return runtimecontract.ToolResult{Content: toolErrorContent(code, message), ErrorClass: code}
}

func toolErrorContent(code, message string) string {
	encoded, _ := json.Marshal(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{Error: code, Message: message})
	return string(encoded)
}

var _ runtimecontract.ToolInvoker = (*Service)(nil)
