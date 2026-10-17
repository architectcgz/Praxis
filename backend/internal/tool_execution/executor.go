// Package toolexecution 提供内部工具授权、执行与持久化结算能力。
package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_execution"
	"praxis/internal/hooks"
	"praxis/internal/repository"
	"praxis/internal/system"
	toolcontracts "praxis/internal/tools/contracts"
)

type TaskRepository interface {
	Get(context.Context, contracts.TaskID) (taskmodel.Task, error)
	Save(context.Context, taskmodel.Task) error
}

type Config struct {
	Rules          ApprovalRules
	Review         func(context.Context, taskmodel.Task, toolmodel.Execution, toolmodel.RiskReview) (securitymodel.RiskAssessment, error)
	HookDispatcher *hooks.Dispatcher
	Transactions   repository.TxRunner
	Tasks          TaskRepository
	Executions     repository.ToolExecutionRepository
	Catalog        toolcontracts.ToolCatalog
	Clock          system.Clock
}

type Executor struct {
	rules           ApprovalRules
	reviewOperation func(context.Context, taskmodel.Task, toolmodel.Execution, toolmodel.RiskReview) (securitymodel.RiskAssessment, error)
	reviewInFlight  sync.Map
	hookDispatcher  *hooks.Dispatcher
	tx              repository.TxRunner
	tasks           TaskRepository
	executions      repository.ToolExecutionRepository
	toolCatalog     toolcontracts.ToolCatalog
	clock           system.Clock
}

func NewExecutor(config Config) (*Executor, error) {
	if config.Rules.DefaultMode == nil || config.Rules.Catalog == nil || config.Review == nil {
		return nil, errors.New("tool executor approval rules and risk reviewer are required")
	}
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"tasks":        config.Tasks,
		"executions":   config.Executions,
		"tool catalog": config.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool executor %s is required", name)
		}
	}
	return &Executor{
		hookDispatcher:  config.HookDispatcher,
		rules:           config.Rules,
		reviewOperation: config.Review,
		tx:              config.Transactions,
		tasks:           config.Tasks,
		executions:      config.Executions,
		toolCatalog:     config.Catalog,
		clock:           system.ClockOrDefault(config.Clock),
	}, nil
}

var errApprovalPending = errors.New("tool approval pending")

// Execute 返回可靠结算结果或审批等待标记。
func (s *Executor) Execute(ctx context.Context, prepared toolcontracts.PreparedToolCall, metadata contracts.ToolExecutionMetadata) (toolcontracts.ToolExecutionResult, error) {
	result, err := s.invoke(ctx, prepared, metadata)
	if errors.Is(err, errApprovalPending) {
		return toolcontracts.ToolExecutionResult{ApprovalPending: true}, nil
	}
	return toolcontracts.ToolExecutionResult{Result: result}, err
}

func (s *Executor) invoke(
	ctx context.Context,
	prepared toolcontracts.PreparedToolCall,
	metadata contracts.ToolExecutionMetadata,
) (toolcontracts.ToolResult, error) {
	if ctx == nil {
		return toolcontracts.ToolResult{}, errors.New("tool invocation metadata is required")
	}
	call := prepared.Call.Snapshot()
	if call.ID == "" || !call.Name.Valid() || len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
		return toolcontracts.NewToolError("invalid_tool_call", "The tool call is invalid."), nil
	}
	if metadata.TaskID == "" || metadata.TurnID == "" ||
		metadata.SessionID == "" || metadata.AgentID == "" {
		return toolcontracts.ToolResult{}, errors.New("tool invocation ownership is required")
	}
	err := s.checkTask(ctx, metadata)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	invocation, err := s.executions.FindByTaskCall(ctx, metadata.TaskID, call.ID)
	if err != nil {
		return toolcontracts.ToolResult{}, err
	}
	if invocation.AgentID != metadata.AgentID || invocation.SessionID != metadata.SessionID ||
		invocation.TurnID != metadata.TurnID || invocation.Name != call.Name {
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
		if errors.Is(err, contracts.New(contracts.TaskResourceLimit, "")) {
			result, settleErr := s.reject(ctx, invocation.ID, toolmodel.FailureResourceLimit, "Tool call limit exceeded.")
			if settleErr != nil {
				return toolcontracts.ToolResult{}, settleErr
			}
			return result, nil
		}
		return toolcontracts.ToolResult{}, err
	}
	invalid := func(message string) (toolcontracts.ToolResult, error) {
		return s.reject(ctx, invocation.ID, toolmodel.FailureInvalidCall, message)
	}
	tool, ok := s.toolCatalog.Get(call.Name)
	if !ok {
		return s.reject(ctx, invocation.ID, toolmodel.FailureNotAllowed, "The requested tool is disabled or unregistered.")
	}
	normalized := toolcontracts.NormalizedToolCall{Name: invocation.Name, NormalizedArguments: append(json.RawMessage(nil), invocation.NormalizedArguments...), Path: invocation.Path}
	if invocation.Status == toolmodel.StatusRequested {
		if prepared.Err != nil {
			return invalid(prepared.Err.Error())
		}
		normalized = prepared.Normalized
		if normalized.Name != call.Name || len(normalized.NormalizedArguments) == 0 || !json.Valid(normalized.NormalizedArguments) {
			return invalid("The tool arguments are invalid.")
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return toolcontracts.ToolResult{}, err
		}
		invocation, err = s.admit(ctx, invocation, normalized)
		if err == nil {
			invocation, err = s.authorize(ctx, invocation.ID)
		}
		if err != nil {
			return toolcontracts.ToolResult{}, err
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		var execute bool
		invocation, execute, err = s.start(ctx, invocation.ID)
		if err != nil {
			return toolcontracts.ToolResult{}, err
		}
		if invocation.Status == toolmodel.StatusAwaitingApproval {
			return toolcontracts.ToolResult{}, errApprovalPending
		}
		// 配置可能在批准与启动两个提交之间改变，由 Core 撤销旧授权后重新编排。
		if invocation.Status == toolmodel.StatusPrepared {
			continue
		}
		if execute {
			break
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		return toolcontracts.ToolResult{}, contracts.New(contracts.TaskBusy, "tool call is already running or being reviewed")
	}
	execution := toolcontracts.ExecutionCall{
		Name:                invocation.Name,
		NormalizedArguments: append(json.RawMessage(nil), invocation.NormalizedArguments...),
		Path:                invocation.Path,
		WorkspacePath:       metadata.WorkspacePath,
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
	result, executeErr := tool.Execute(toolCtx, execution)
	scope.End(hooks.Result{Err: executeErr, ToolErrorClass: result.ErrorClass})
	if executeErr != nil {
		if ctx.Err() != nil {
			interrupted := toolcontracts.NewToolError(string(toolmodel.FailureResultUnknown), "The tool was interrupted after execution started. Its side effects are unknown; do not automatically retry.")
			if err := s.interrupt(ctx, invocation.ID, interrupted); err != nil {
				return toolcontracts.ToolResult{}, err
			}
			return interrupted, ctx.Err()
		}
		failed := toolcontracts.NewToolError(string(toolmodel.FailureExecutionFailed), "The tool could not be executed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return toolcontracts.ToolResult{}, err
		}
		return failed, nil
	}
	if result.ErrorClass != "" {
		failed := toolcontracts.NewToolError(string(toolmodel.FailureExecutionFailed), "The tool could not be executed.")
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
