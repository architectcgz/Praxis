// Package toolexecution 编排工具执行的准入、授权、风险审核与持久化结算。
package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/repository"
	agentservice "praxis/internal/service/agent"
	"sync"
)

type Dependencies struct {
	Rules ApprovalRules
	// Review 在模型响应边界完成规范化与校验，成功时只返回 canonical 结论。
	Review         func(context.Context, agentmodel.Execution, executionmodel.Execution, executionmodel.RiskReview) (security.RiskAssessment, error)
	HookDispatcher *hook.Dispatcher
	Transactions   repository.Transaction
	Tasks          repository.AgentExecution
	Executions     repository.ToolExecution
	Catalog        tool.ToolCatalog
	Clock          clock.Clock
}

type Executor struct {
	rules           ApprovalRules
	reviewOperation func(context.Context, agentmodel.Execution, executionmodel.Execution, executionmodel.RiskReview) (security.RiskAssessment, error)
	reviewInFlight  sync.Map
	hookDispatcher  *hook.Dispatcher
	tx              repository.Transaction
	tasks           repository.AgentExecution
	executions      repository.ToolExecution
	toolCatalog     tool.ToolCatalog
	clock           clock.Clock
}

func NewExecutor(deps Dependencies) (*Executor, error) {
	if deps.Rules.DefaultMode == nil || deps.Rules.Catalog == nil || deps.Review == nil {
		return nil, errors.New("tool executor approval rules and risk reviewer are required")
	}
	for name, value := range map[string]any{
		"transactions": deps.Transactions,
		"tasks":        deps.Tasks,
		"executions":   deps.Executions,
		"tool catalog": deps.Catalog,
	} {
		if value == nil {
			return nil, fmt.Errorf("tool executor %s is required", name)
		}
	}
	return &Executor{
		hookDispatcher:  deps.HookDispatcher,
		rules:           deps.Rules,
		reviewOperation: deps.Review,
		tx:              deps.Transactions,
		tasks:           deps.Tasks,
		executions:      deps.Executions,
		toolCatalog:     deps.Catalog,
		clock:           clock.ClockOrDefault(deps.Clock),
	}, nil
}

// Execute 返回可靠结算结果或审批等待标记。
func (s *Executor) Execute(ctx context.Context, prepared tool.PreparedToolCall, metadata executionmodel.ToolExecutionMetadata) (tool.ToolExecutionResult, error) {
	result, err := s.invoke(ctx, prepared, metadata)
	if errors.Is(err, errApprovalPending) {
		return tool.ToolExecutionResult{ApprovalPending: true}, nil
	}
	return tool.ToolExecutionResult{Result: result}, err
}

func (s *Executor) invoke(
	ctx context.Context,
	prepared tool.PreparedToolCall,
	metadata executionmodel.ToolExecutionMetadata,
) (tool.ToolResult, error) {
	if ctx == nil {
		return tool.ToolResult{}, errors.New("tool invocation metadata is required")
	}
	call := prepared.Call.Snapshot()
	if metadata.ExecutionID == "" || metadata.TurnID == "" ||
		metadata.SessionID == "" || metadata.AgentID == "" {
		return tool.ToolResult{}, errors.New("tool invocation ownership is required")
	}
	err := s.checkTask(ctx, metadata)
	if err != nil {
		return tool.ToolResult{}, err
	}
	invocation, err := s.executions.FindByExecutionCall(ctx, metadata.ExecutionID, call.ID)
	if err != nil {
		return tool.ToolResult{}, err
	}
	if invocation.AgentID != metadata.AgentID || invocation.SessionID != metadata.SessionID ||
		invocation.TurnID != metadata.TurnID || invocation.Name != call.Name {
		return tool.ToolResult{}, fault.ErrRequestConflict
	}
	if invocation.ArgumentsDigest != executionmodel.ArgumentsDigest(call.Name, call.Arguments) {
		return tool.NewToolError("tool_request_conflict", "The tool call identity was reused with different arguments."), nil
	}
	if result, done := settledResult(invocation); done {
		return result, nil
	}
	invocation, err = s.reserveCall(ctx, invocation.ID)
	if err != nil {
		return tool.ToolResult{}, err
	}
	invalid := func(message string) (tool.ToolResult, error) {
		return s.reject(ctx, invocation.ID, executionmodel.FailureInvalidCall, message)
	}
	registeredTool, ok := s.toolCatalog.Get(call.Name)
	if !ok {
		return s.reject(ctx, invocation.ID, executionmodel.FailureNotAllowed, "The requested tool is disabled or unregistered.")
	}
	normalized := tool.NormalizedToolCall{Name: invocation.Name, NormalizedArguments: append(json.RawMessage(nil), invocation.NormalizedArguments...), Path: invocation.Path}
	if invocation.Status == executionmodel.StatusRequested {
		if prepared.Err != nil {
			return invalid(prepared.Err.Error())
		}
		normalized = prepared.Normalized
		if normalized.Name != call.Name {
			return invalid("The tool arguments are invalid.")
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return tool.ToolResult{}, err
		}
		invocation, err = s.admit(ctx, invocation, normalized)
		if err == nil {
			invocation, err = s.authorize(ctx, invocation.ID)
		}
		if err != nil {
			return tool.ToolResult{}, err
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		var execute bool
		invocation, execute, err = s.start(ctx, invocation.ID)
		if err != nil {
			return tool.ToolResult{}, err
		}
		if invocation.Status == executionmodel.StatusAwaitingApproval {
			return tool.ToolResult{}, errApprovalPending
		}
		// 配置可能在批准与启动两个提交之间改变，由 Core 撤销旧授权后重新编排。
		if invocation.Status == executionmodel.StatusPrepared {
			continue
		}
		if execute {
			break
		}
		if result, done := settledResult(invocation); done {
			return result, nil
		}
		return tool.ToolResult{}, fault.New(agentservice.ExecutionBusy, "tool call is already running or being reviewed")
	}
	execution := tool.ExecutionCall{
		Name:                invocation.Name,
		NormalizedArguments: append(json.RawMessage(nil), invocation.NormalizedArguments...),
		Path:                invocation.Path,
		WorkspacePath:       metadata.WorkspacePath,
	}
	toolCtx, scope := s.hookDispatcher.Start(ctx, hook.Operation{
		Identity: hook.Identity{
			SessionID:   invocation.SessionID,
			AgentID:     invocation.AgentID,
			ExecutionID: invocation.ExecutionID,
			TurnID:      invocation.TurnID,
		},
		Kind: hook.OperationTool, Name: string(call.Name), ReferenceID: call.ID,
	})
	result, executeErr := registeredTool.Execute(toolCtx, execution)
	scope.End(hook.Result{Err: executeErr, ToolErrorClass: result.ErrorClass})
	if executeErr != nil {
		if ctx.Err() != nil {
			interrupted := tool.NewToolError(string(executionmodel.FailureResultUnknown), "The tool was interrupted after execution started. Its side effects are unknown; do not automatically retry.")
			if err := s.interrupt(ctx, invocation.ID, interrupted); err != nil {
				return tool.ToolResult{}, err
			}
			return interrupted, ctx.Err()
		}
		failed := tool.NewToolError(string(executionmodel.FailureExecutionFailed), "The tool could not be executed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return tool.ToolResult{}, err
		}
		return failed, nil
	}
	if result.ErrorClass != "" {
		failed := tool.NewToolError(string(executionmodel.FailureExecutionFailed), "The tool could not be executed.")
		if err := s.fail(ctx, invocation.ID, failed); err != nil {
			return tool.ToolResult{}, err
		}
		return failed, nil
	}
	result.ErrorClass = ""
	if err := s.succeed(ctx, invocation.ID, result); err != nil {
		return tool.ToolResult{}, err
	}
	return result, nil
}
