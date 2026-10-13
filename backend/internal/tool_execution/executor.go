// Package toolexecution 提供内部工具授权、执行与持久化结算能力。
package toolexecution

import (
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	toolmodel "praxis/internal/core/tool_invocation"
	"strings"

	"praxis/internal/repository"
	"praxis/internal/system"
	toolcontracts "praxis/internal/tools/contracts"
)

type TaskRepository interface {
	Get(context.Context, contracts.TaskID) (taskmodel.Task, error)
	Save(context.Context, taskmodel.Task) error
}

type CurrentSecurityResolver interface {
	ResolveTaskSecurity(context.Context, taskmodel.Task) (contracts.SecuritySnapshot, error)
}

type Config struct {
	CurrentSecurity CurrentSecurityResolver
	HookDispatcher  *hooks.Dispatcher
	Transactions    repository.TxRunner
	Tasks           TaskRepository
	Invocations     repository.ToolInvocationRepository
	Catalog         toolcontracts.ToolCatalog
	Clock           system.Clock
}

type Executor struct {
	currentSecurity CurrentSecurityResolver
	hookDispatcher  *hooks.Dispatcher
	tx              repository.TxRunner
	tasks           TaskRepository
	invocations     repository.ToolInvocationRepository
	toolCatalog     toolcontracts.ToolCatalog
	clock           system.Clock
}

func NewExecutor(config Config) (*Executor, error) {
	for name, value := range map[string]any{
		"current security": config.CurrentSecurity,
		"transactions":     config.Transactions,
		"tasks":            config.Tasks,
		"invocations":      config.Invocations,
		"tool catalog":     config.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool executor %s is required", name)
		}
	}
	return &Executor{
		hookDispatcher:  config.HookDispatcher,
		currentSecurity: config.CurrentSecurity,
		tx:              config.Transactions, tasks: config.Tasks,
		invocations: config.Invocations,
		toolCatalog: config.Catalog,
		clock:       system.ClockOrDefault(config.Clock),
	}, nil
}

var errApprovalPending = errors.New("tool approval pending")

// Execute 返回可靠结算结果或审批等待标记。
func (s *Executor) Execute(ctx context.Context, call contracts.ToolCall, metadata contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error) {
	result, err := s.invoke(ctx, call, metadata)
	if errors.Is(err, errApprovalPending) {
		return toolcontracts.ToolExecutionResult{ApprovalPending: true}, nil
	}
	return toolcontracts.ToolExecutionResult{Result: result}, err
}

func (s *Executor) invoke(
	ctx context.Context,
	call contracts.ToolCall,
	invocationContext contracts.ToolInvocationContext,
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
	security, err := s.loadSecurity(ctx, invocationContext)
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
	if invocation.ArgumentsDigest != toolmodel.ArgumentsDigest(call.Name, call.Arguments) {
		return toolcontracts.NewToolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
	}
	if result, done := settledResult(invocation); done {
		return result, nil
	}
	invocation, err = s.reserveCall(ctx, invocation.ID)
	if err != nil {
		return toolcontracts.ToolResult{}, err
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
	normalized := toolcontracts.NormalizedToolCall{Name: invocation.Name, NormalizedArguments: append(json.RawMessage(nil), invocation.NormalizedArguments...), Path: invocation.Path}
	if invocation.Status == toolmodel.ToolInvocationRequested {

		normalized, err = tool.Normalize(call)
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
	invocation, execute, err := s.start(ctx, invocation.ID, normalized.Path, &security)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if invocation.Status == toolmodel.ToolInvocationAwaitingApproval {
		return toolcontracts.ToolResult{}, errApprovalPending
	}
	if !execute {
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		return toolcontracts.ToolResult{}, contracts.New(contracts.TaskBusy, "tool call is already running")
	}
	authorized := toolcontracts.AuthorizedToolCall{
		Name:                normalized.Name,
		NormalizedArguments: append(json.RawMessage(nil), normalized.NormalizedArguments...),
		Path:                normalized.Path,
		ReadScopes:          append([]string(nil), security.Permissions.ReadScopes...),
		WriteScopes:         append([]string(nil), security.Permissions.WriteScopes...),
		AllowedExecutables:  append([]string(nil), security.Permissions.AllowedExecutables...),
	}
	toolCtx, scope := s.hookDispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: invocation.SessionID,
			AgentID:   invocation.AgentID,
			TaskID:    invocation.TaskID,
			TurnID:    invocation.TurnID,
		},
		Kind: hooks.OperationTool, Name: string(call.Name), ReferenceID: call.ID,
	})
	result, executeErr := tool.Execute(toolCtx, authorized)
	scope.End(hooks.Result{Err: executeErr, ToolErrorClass: result.ErrorClass})
	if executeErr != nil {
		if ctx.Err() != nil {
			interrupted := toolcontracts.NewToolError(string(toolmodel.ToolFailureResultUnknown), "The tool was interrupted after execution started. Its side effects are unknown; do not automatically retry.")
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
