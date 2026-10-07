package toolinvocation

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_invocation"

	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"praxis/internal/agent_runtime"
	securitymodel "praxis/internal/core/security"
	toolcontracts "praxis/internal/tools/contracts"
)

const settlementTimeout = 5 * time.Second

type invocationIdentity struct {
	Name      contracts.ToolName
	Arguments json.RawMessage
}

// Invoke 只执行已和 assistant 消息原子登记的调用，校验与授权后最多执行一次。
func (s *Service) Invoke(
	ctx context.Context,
	call toolcontracts.ToolCall,
	invocationContext agentruntime.ToolInvocationMetadata,
) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool invocation metadata is required")
	}
	call = call.Snapshot()
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	if call.ID == "" || !call.Name.Valid() || len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
		return toolcontracts.NewToolError("invalid_tool_call", "The tool call is invalid."), nil
	}
	if invocationContext.TaskID == "" || invocationContext.TurnID == "" ||
		invocationContext.SessionID == "" || invocationContext.AgentID == "" {
		return toolcontracts.ToolResult{}, errors.New("tool invocation ownership is required")
	}
	security, err := s.loadModelContext(ctx, invocationContext)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	invocation, err := s.invocations.FindByTaskCall(ctx, invocationContext.TaskID, call.ID)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if invocation.AgentID != invocationContext.AgentID || invocation.SessionID != invocationContext.SessionID ||
		invocation.TurnID != invocationContext.TurnID || invocation.Name != call.Name {
		return toolcontracts.ToolResult{}, contracts.ErrRequestConflict
	}
	if len(invocation.Arguments) > 0 && invocation.ArgumentsDigest != toolCallDigest(invocationIdentity{Name: call.Name, Arguments: call.Arguments}) {
		return toolcontracts.NewToolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
	}
	if result, done := settledResult(invocation); done {
		return result, nil
	}
	invalid := func(message string) (toolcontracts.ToolResult, error) {
		result := toolcontracts.NewToolError(string(toolmodel.ToolFailureInvalidCall), message)
		err := s.settle(ctx, invocation.ID, func(current *toolmodel.ToolInvocation) error {
			return current.Deny(toolmodel.ToolInvocationResult{
				InlineContent: result.Payload,
				ErrorCode:     toolmodel.ToolFailureInvalidCall,
			}, s.clock.Now())
		})
		return result, err
	}
	tool, ok := s.toolCatalog.Get(call.Name)
	if !ok {
		return invalid("The requested tool is not available.")
	}
	normalized, err := tool.Normalize(call)
	if err != nil {
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "The tool arguments are invalid."
		}
		return invalid(message)
	}
	if normalized.Name != call.Name || len(normalized.NormalizedArguments) == 0 ||
		!json.Valid(normalized.NormalizedArguments) {
		return toolcontracts.ToolResult{}, errors.New("tool catalog returned invalid normalized arguments")
	}
	normalized, err = resolveNormalizedPath(normalized, invocationContext.WorkspacePath)
	if err != nil {
		return invalid("The tool path is invalid.")
	}
	if len(invocation.Arguments) == 0 && invocation.ArgumentsDigest != toolCallDigest(invocationIdentity{Name: call.Name, Arguments: normalized.NormalizedArguments}) {
		return toolcontracts.NewToolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
	}
	if len(invocation.NormalizedArguments) > 0 && !bytes.Equal(invocation.NormalizedArguments, normalized.NormalizedArguments) {
		return toolcontracts.NewToolError("tool_request_conflict", "The normalized arguments differ from the approved tool call."), nil
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
			"",
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

func (s *Service) loadModelContext(
	ctx context.Context,
	provided agentruntime.ToolInvocationMetadata,
) (contracts.SecuritySnapshot, error) {
	task, err := s.tasks.Get(ctx, provided.TaskID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if !task.Active() || task.SessionID != provided.SessionID || task.AgentID != provided.AgentID {
		return contracts.SecuritySnapshot{}, errors.New(
			"tool invocation task is not active or ownership does not match",
		)
	}
	security, err := s.security.Get(ctx, provided.TaskID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if security.Fingerprint != task.Input.Security.Fingerprint ||
		security.Fingerprint != provided.SecurityFingerprint ||
		provided.WorkspacePath != task.Input.WorkspacePath {
		return contracts.SecuritySnapshot{}, errors.New(
			"tool invocation security snapshot does not match the active task",
		)
	}
	return security, nil
}

// toolCallDigest 生成原始调用参数的稳定指纹，重试不得替换已登记的意图。
func toolCallDigest(value invocationIdentity) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// recordCall 只在 assistant 保存事务中登记调用，重复身份必须保持原始意图和归属一致。
func (s *Service) recordCall(
	ctx context.Context,
	call toolcontracts.ToolCall,
	provided agentruntime.ToolInvocationMetadata,
) error {
	digest := toolCallDigest(invocationIdentity{Name: call.Name, Arguments: call.Arguments})
	if digest == "" {
		return errors.New("tool invocation arguments could not be fingerprinted")
	}
	existing, err := s.invocations.FindByTaskCall(ctx, provided.TaskID, call.ID)
	if err == nil {
		if existing.Name != call.Name || existing.ArgumentsDigest != digest || existing.TurnID != provided.TurnID ||
			existing.AgentID != provided.AgentID || existing.SessionID != provided.SessionID {
			return contracts.ErrRequestConflict
		}
		return nil
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		return err
	}
	invocation, err := toolmodel.NewToolInvocation(
		contracts.ToolInvocationID(s.ids.New("toolinvocation")), provided.TaskID,
		provided.TurnID, provided.SessionID, provided.AgentID, call.ID, call.Name,
		call.Arguments, digest, s.clock.Now(),
	)
	if err != nil {
		return err
	}
	return s.invocations.Save(ctx, invocation)
}

func (s *Service) admit(
	ctx context.Context,
	invocation toolmodel.ToolInvocation,
	security contracts.SecuritySnapshot,
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
			current.NormalizedArguments = append(json.RawMessage(nil), call.NormalizedArguments...)
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
	if workspacePath == "" {
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

var _ agentruntime.ToolCallHandler = (*Service)(nil)
