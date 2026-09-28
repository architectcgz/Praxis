package toolinvocation

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/tool_invocation"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	runtimecontract "praxis/internal/runtime"
	securitymodel "praxis/internal/security"
	toolcontracts "praxis/internal/tools/contracts"
)

const settlementTimeout = 5 * time.Second

type invocationIdentity struct {
	Name      contracts.ToolName
	Arguments json.RawMessage
}

// Invoke validates the runtime context, durably admits the call, and executes it at most once.
func (s *Service) Invoke(
	ctx context.Context,
	call toolcontracts.ToolCall,
	invocationContext runtimecontract.ToolInvocationMetadata,
) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool invocation metadata is required")
	}
	call = call.Snapshot()
	if strings.TrimSpace(call.ID) == "" || !call.Name.Valid() || len(call.Input) == 0 || !json.Valid(call.Input) {
		return toolcontracts.NewToolError("invalid_tool_call", "The tool call is invalid."), nil
	}
	security, err := s.loadExecutionContext(ctx, invocationContext)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	tool, ok := s.catalog.Get(call.Name)
	if !ok {
		return toolcontracts.NewToolError("invalid_tool_call", "The requested tool is not available."), nil
	}
	normalized, err := tool.Normalize(call)
	if err != nil {
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "The tool arguments are invalid."
		}
		return toolcontracts.NewToolError("invalid_tool_call", message), nil
	}
	normalized = normalized.Snapshot()
	if normalized.Name != call.Name || len(normalized.NormalizedArguments) == 0 ||
		!json.Valid(normalized.NormalizedArguments) {
		return toolcontracts.ToolResult{}, errors.New("tool catalog returned invalid normalized arguments")
	}
	normalized, err = resolveNormalizedPath(normalized, invocationContext.WorkspacePath)
	if err != nil {
		return toolcontracts.NewToolError("invalid_tool_call", "The tool path is invalid."), nil
	}
	digest := toolCallDigest(invocationIdentity{
		Name:      call.Name,
		Arguments: normalized.NormalizedArguments,
	})
	if digest == "" {
		return toolcontracts.ToolResult{}, errors.New("tool invocation arguments could not be fingerprinted")
	}
	invocation, created, err := s.findOrCreate(ctx, call, invocationContext, normalized, digest)
	if err != nil {
		if errors.Is(err, contracts.ErrRequestConflict) {
			return toolcontracts.NewToolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
		}
		return toolcontracts.ToolResult{}, err
	}
	if !created {
		if result, done := settledResult(invocation); done {
			return result, nil
		}
	}
	if invocation.Status == toolmodel.ToolInvocationRequested {
		invocation, err = s.admit(ctx, invocation, security, normalized)
		if err != nil {
			return toolcontracts.ToolResult{}, err
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
	}
	invocation, execute, err := s.start(ctx, invocation.ID)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if !execute {
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		return toolcontracts.NewToolError(
			string(toolmodel.ToolFailureResultUnknown),
			"The tool result is unavailable and the call was not replayed.",
		), nil
	}
	authorized := toolcontracts.AuthorizedToolCall{
		Name:                normalized.Name,
		NormalizedArguments: append(json.RawMessage(nil), normalized.NormalizedArguments...),
		Path:                normalized.Path,
		ReadScopes:          append([]string(nil), security.Permissions.ReadScopes...),
		WriteScopes:         append([]string(nil), security.Permissions.WriteScopes...),
		AllowedExecutables:  append([]string(nil), security.Permissions.AllowedExecutables...),
	}
	result, executeErr := tool.Execute(ctx, authorized)
	if executeErr != nil {
		if ctx.Err() != nil {
			interrupted := toolcontracts.NewToolError(string(toolmodel.ToolFailureInterrupted), "The tool call was interrupted.")
			if err := s.interrupt(ctx, invocation.ID, interrupted); err != nil {
				return toolcontracts.ToolResult{}, err
			}
			return interrupted, ctx.Err()
		}
		failed := toolcontracts.NewToolError(string(toolmodel.ToolFailureExecutionFailed), "The tool could not be executed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return toolcontracts.ToolResult{}, err
		}
		return failed, nil
	}
	if result.ErrorClass != "" {
		failed := toolcontracts.NewToolError(string(toolmodel.ToolFailureExecutionFailed), "The tool could not be executed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return toolcontracts.ToolResult{}, err
		}
		return failed, nil
	}
	result.ErrorClass = ""
	if err := s.succeed(ctx, invocation.ID, result); err != nil {
		return toolcontracts.ToolResult{}, err
	}
	return result, nil
}

func (s *Service) loadExecutionContext(
	ctx context.Context,
	provided runtimecontract.ToolInvocationMetadata,
) (contracts.ExecutionSecuritySnapshot, error) {
	if provided.ExecutionID == "" || provided.SessionID == "" || provided.AgentID == "" {
		return contracts.ExecutionSecuritySnapshot{}, errors.New("tool invocation ownership is required")
	}
	execution, err := s.executions.Get(ctx, provided.ExecutionID)
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if !execution.Active() || execution.SessionID != provided.SessionID || execution.AgentID != provided.AgentID {
		return contracts.ExecutionSecuritySnapshot{}, errors.New(
			"tool invocation execution is not active or ownership does not match",
		)
	}
	security, err := s.security.Get(ctx, provided.ExecutionID)
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if security.Fingerprint != execution.Input.Security.Fingerprint ||
		security.Fingerprint != provided.Execution.Revision ||
		provided.Execution != execution.Input.Runtime ||
		filepath.Clean(provided.WorkspacePath) != filepath.Clean(execution.Input.WorkspacePath) {
		return contracts.ExecutionSecuritySnapshot{}, errors.New(
			"tool invocation security snapshot does not match the active execution",
		)
	}
	return security, nil
}

// toolCallDigest 生成同一 provider tool call 的规范化参数指纹。
func toolCallDigest(value invocationIdentity) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (s *Service) findOrCreate(
	ctx context.Context,
	call toolcontracts.ToolCall,
	provided runtimecontract.ToolInvocationMetadata,
	normalized toolcontracts.NormalizedToolCall,
	digest string,
) (toolmodel.ToolInvocation, bool, error) {
	var invocation toolmodel.ToolInvocation
	created := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.invocations.FindByExecutionCall(txCtx, provided.ExecutionID, call.ID)
		if err == nil {
			if existing.Name != call.Name || existing.ArgumentsDigest != digest {
				return contracts.ErrRequestConflict
			}
			invocation = existing
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		invocation, err = toolmodel.NewToolInvocation(
			contracts.ToolInvocationID(s.ids.New("toolinvocation")), provided.ExecutionID,
			provided.SessionID, provided.AgentID, call.ID, call.Name,
			normalized.NormalizedArguments, digest, s.clock.Now(),
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
	invocation toolmodel.ToolInvocation,
	security contracts.ExecutionSecuritySnapshot,
	call toolcontracts.NormalizedToolCall,
) (toolmodel.ToolInvocation, error) {
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, invocation.ID)
		if err != nil {
			return err
		}
		if current.Status != toolmodel.ToolInvocationRequested {
			invocation = current
			return nil
		}
		allowed := securitymodel.AllowsToolCall(security, current.Name, call.Path)
		if !allowed {
			deniedResult := toolcontracts.NewToolError(
				string(toolmodel.ToolFailureNotAllowed),
				"The requested path is outside the authorized read scope.",
			)
			denied := toolmodel.ToolInvocationResult{
				InlineContent: deniedResult.Payload,
				ErrorCode:     toolmodel.ToolFailureNotAllowed,
			}
			if err := current.Deny(denied, s.clock.Now()); err != nil {
				return err
			}
		} else {
			approval, err := contracts.NewPolicyApproval(security.Fingerprint, s.clock.Now())
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
	id contracts.ToolInvocationID,
) (toolmodel.ToolInvocation, bool, error) {
	var invocation toolmodel.ToolInvocation
	execute := false
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		current, err := s.invocations.Get(txCtx, id)
		if err != nil {
			return err
		}
		if current.Status == toolmodel.ToolInvocationApproved {
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
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, SideEffect: result.SideEffect, Truncated: result.Truncated,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.Succeed(domainResult, s.clock.Now())
	})
}

func (s *Service) fail(
	ctx context.Context,
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureExecutionFailed,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.Fail(domainResult, toolmodel.ToolFailureExecutionFailed, s.clock.Now())
	})
}

func (s *Service) interrupt(
	ctx context.Context,
	id contracts.ToolInvocationID,
	result toolcontracts.ToolResult,
) error {
	domainResult := toolmodel.ToolInvocationResult{
		InlineContent: result.Payload, ErrorCode: toolmodel.ToolFailureInterrupted,
	}
	return s.settle(ctx, id, func(invocation *toolmodel.ToolInvocation) error {
		return invocation.Interrupt(domainResult, s.clock.Now())
	})
}

func (s *Service) settle(
	ctx context.Context,
	id contracts.ToolInvocationID,
	transition func(*toolmodel.ToolInvocation) error,
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

func settledResult(invocation toolmodel.ToolInvocation) (toolcontracts.ToolResult, bool) {
	if !invocation.Status.Terminal() {
		return toolcontracts.ToolResult{}, false
	}
	return toolcontracts.ToolResult{
		Payload: invocation.Result.InlineContent, ErrorClass: string(invocation.Result.ErrorCode),
		SideEffect: invocation.Result.SideEffect, Truncated: invocation.Result.Truncated,
	}, true
}

func resolveNormalizedPath(
	call toolcontracts.NormalizedToolCall,
	workspacePath string,
) (toolcontracts.NormalizedToolCall, error) {
	if call.Path == "" {
		return call, nil
	}
	if strings.TrimSpace(workspacePath) == "" {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool workspace path is required")
	}
	path := call.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspacePath, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Clean(absolute) == "." {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path is invalid")
	}
	call.Path = filepath.Clean(absolute)
	arguments := make(map[string]json.RawMessage)
	if err := json.Unmarshal(call.NormalizedArguments, &arguments); err != nil {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path arguments are invalid")
	}
	encodedPath, err := json.Marshal(call.Path)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, err
	}
	arguments["path"] = encodedPath
	call.NormalizedArguments, err = json.Marshal(arguments)
	if err != nil {
		return toolcontracts.NormalizedToolCall{}, errors.New("tool path arguments could not be encoded")
	}
	return call, nil
}

var _ runtimecontract.ToolCallHandler = (*Service)(nil)
