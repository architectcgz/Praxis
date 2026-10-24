package toolexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/clock"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"sync"
	"time"
)

type ExecutorDependencies struct {
	Catalog        tool.ToolCatalog
	Operations     Operations
	Reviewer       *Reviewer
	Prepare        func(tool.Tool, json.RawMessage, string) (json.RawMessage, error)
	Bind           func(tool.Tool, string) tool.Tool
	HookDispatcher *hook.Dispatcher
	TimingRecorder *timing.Recorder
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	Logf           func(string, ...any)
}

// Executor 复用工具能力，并同步工具启动、人工决定及取消；不保存审批状态机。
type Executor struct {
	deps   ExecutorDependencies
	mu     sync.Mutex
	active map[Key]*Execution
}

func NewExecutor(deps ExecutorDependencies) *Executor {
	return &Executor{deps: deps, active: make(map[Key]*Execution)}
}

// ExecutionTools 只绑定本次已取得运行权的 AgentExecution 的环境。
type ExecutionTools struct {
	executor      *Executor
	workspacePath string
	awaitApproval func(context.Context) error
}

func (e *Executor) ForExecution(workspacePath string, awaitApproval func(context.Context) error) *ExecutionTools {
	return &ExecutionTools{
		executor:      e,
		workspacePath: workspacePath,
		awaitApproval: awaitApproval,
	}
}

// Execute 在同一次调用中准备参数、授权、等待审批、执行工具并提交回执。
func (tools *ExecutionTools) Execute(ctx context.Context, call sessionentry.ToolCall) (sessionentry.ContextEntry, error) {
	s := tools.executor
	if stored, found, err := s.deps.Operations.GetResult(ctx, call.Key); err != nil || found {
		return stored, err
	}
	if err := ctx.Err(); err != nil {
		return s.appendError(ctx, call, "tool_interrupted", err.Error())
	}
	t, ok := s.deps.Catalog.Get(call.Name)
	if !ok {
		return s.appendError(ctx, call, "tool_not_allowed", "The requested tool is disabled or unregistered.")
	}
	boundary := toolHookEvent(call, call.Arguments)
	boundary.OperationID = hook.NewOperationID()
	modified, hookErr := s.deps.HookDispatcher.BeforeToolExecution(ctx, boundary)
	if hookErr != nil {
		return sessionentry.ContextEntry{}, hookErr
	}
	if err := ctx.Err(); err != nil {
		return s.appendError(ctx, call, "tool_interrupted", err.Error())
	}
	call.Arguments = bytes.Clone(modified)
	args, err := s.deps.Prepare(t, call.Arguments, tools.workspacePath)
	if err != nil {
		return s.appendError(ctx, call, "invalid_tool_call", err.Error())
	}
	boundary.Arguments = bytes.Clone(args)
	// 每次 ToolCall 只读取一次内存规则，审批与启动均沿用本次决定。
	decision, err := s.deps.Operations.DecideRule(ctx, call, args)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	return tools.execute(ctx, call, t, bytes.Clone(args), decision, boundary)
}

func (tools *ExecutionTools) execute(ctx context.Context, call sessionentry.ToolCall, t tool.Tool, args json.RawMessage, decision security.ApprovalDecision, boundary hook.ToolExecution) (sessionentry.ContextEntry, error) {
	s := tools.executor

	for {
		if stored, found, err := s.deps.Operations.GetResult(ctx, call.Key); err != nil || found {
			return stored, err
		}
		pending, denied, err := tools.authorize(ctx, call, args, decision)
		if err != nil {
			if errors.Is(err, errResultAvailable) {
				continue
			}
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}
			return sessionentry.ContextEntry{}, err
		}
		if denied != nil {
			return s.commitResult(ctx, call, *denied)
		}

		if pending {
			// 请求已可靠保存；通知和等待均在控制锁外，等待信号保留提前到达的审批决定或停止通知。
			sourceTaskID, err := s.checkCall(ctx, call)
			if err != nil {
				return sessionentry.ContextEntry{}, err
			}
			waiting := boundary
			waiting.SourceTaskID = sourceTaskID
			waiting.Call.Arguments = bytes.Clone(args)
			s.notifyWaiting(ctx, waiting)
			if err := tools.awaitApproval(ctx); err != nil {
				return sessionentry.ContextEntry{}, err
			}
			continue
		}

		observed := observedTool{
			Tool:     s.deps.Bind(t, tools.workspacePath),
			recorder: s.deps.TimingRecorder,
			hooks:    s.deps.HookDispatcher,
			identity: hook.Identity{
				OperationID: boundary.OperationID,
				SessionID:   call.SessionID,
				AgentID:     call.AgentID,
				ExecutionID: call.ExecutionID,
				TurnID:      call.TurnID,
			},
			callID: call.CallID,
		}
		execution := NewExecution(ctx, observed, args)
		err = s.register(ctx, call, execution)
		if err != nil {
			execution.Stop()
			if errors.Is(err, errResultAvailable) {
				continue
			}
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}

			return sessionentry.ContextEntry{}, err
		}
		// active 仅供 Stop 查找取消句柄；本次 Execute 返回时移除。
		defer s.unregister(call.Key)
		result := execution.Run()

		value := result.ToolResult
		if result.Err != nil {
			if value.Payload == "" {
				code := "tool_execution_failed"
				if errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded) {
					code = "tool_interrupted"
				}
				value = tool.NewToolError(code, result.Err.Error())
			} else if value.ErrorClass == "" {
				value.ErrorClass = "tool_execution_failed"
			}
		}
		modified, hookErr := s.deps.HookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), hook.ToolExecutionResult{
			ToolExecution: boundary,
			Result:        value,
			Err:           result.Err,
		})
		if hookErr == nil && (modified.ErrorClass != value.ErrorClass || modified.SideEffect != value.SideEffect) {
			hookErr = &hook.Error{Point: "AfterToolExecution", Cause: errors.New("tool result hook cannot change execution error or side-effect facts")}
		}
		if hookErr == nil {
			modified.Truncated = modified.Truncated || value.Truncated
			value = modified
		}
		stored, writeErr := s.commitResult(ctx, call, value)
		if writeErr != nil {
			return stored, writeErr
		}

		return stored, hookErr
	}
}

// observedTool 只观察真实调用，不参与授权或结果保存。
type observedTool struct {
	tool.Tool
	recorder *timing.Recorder
	hooks    *hook.Dispatcher
	identity hook.Identity
	callID   string
}

func (t observedTool) Call(ctx context.Context, args json.RawMessage) (result tool.ToolResult, err error) {
	ctx, scope := t.hooks.Start(ctx, hook.Operation{
		Identity:    t.identity,
		Kind:        hook.OperationTool,
		Name:        string(t.Definition().Name),
		ReferenceID: t.callID,
	})
	defer func() { scope.End(hook.Result{Err: err, ToolErrorClass: result.ErrorClass}) }()
	ctx, span := t.recorder.Start(ctx, timing.Operation{
		ExecutionID: t.identity.ExecutionID.String(),
		Kind:        timing.Tool,
		Name:        string(t.Definition().Name),
		ReferenceID: t.callID,
	})
	result, err = t.Tool.Call(ctx, args)
	status := timing.ResultStatus(ctx, err)
	if status == timing.Completed && result.ErrorClass != "" {
		status = timing.Failed
	}
	span.End(status)
	return result, err
}

func toolHookEvent(call sessionentry.ToolCall, args json.RawMessage) hook.ToolExecution {
	return hook.ToolExecution{
		Identity:  hook.Identity{SessionID: call.SessionID, AgentID: call.AgentID, ExecutionID: call.ExecutionID, TurnID: call.TurnID},
		Call:      call,
		Arguments: bytes.Clone(args),
	}
}

func (e *Executor) appendError(ctx context.Context, call sessionentry.ToolCall, code, message string) (sessionentry.ContextEntry, error) {
	return e.commitResult(ctx, call, tool.NewToolError(code, message))
}

func (e *Executor) checkCall(ctx context.Context, call sessionentry.ToolCall) (identity.TaskID, error) {
	_, execution, err := e.deps.Operations.ResolveCall(ctx, ControlRequest{
		SessionID:   call.SessionID,
		AgentID:     call.AgentID,
		ExecutionID: call.ExecutionID,
		CallID:      call.CallID,
	})
	if err != nil {
		return "", err
	}
	if execution.Status != agentexecution.ExecutionRunning {
		return "", errorcode.ErrRequestConflict
	}
	return execution.SourceTaskID, nil
}

func (e *Executor) notifyWaiting(ctx context.Context, value hook.ToolExecution) {
	if err := e.deps.HookDispatcher.ToolExecutionWaiting(context.WithoutCancel(ctx), value); err != nil && e.deps.Logf != nil {
		e.deps.Logf("tool execution waiting hook failed: %v", err)
	}
	e.deps.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.ToolApprovalRequired,
		SessionID:   value.SessionID,
		AgentID:     value.AgentID,
		ExecutionID: value.ExecutionID,
		TurnID:      value.TurnID,
		CallID:      value.Call.CallID,
		Name:        string(value.Call.Name),
	})
}

// commitResult 与 Stop 共享同步边界，迟到的控制请求不覆盖工具回执。
func (e *Executor) commitResult(ctx context.Context, call sessionentry.ToolCall, result tool.ToolResult) (sessionentry.ContextEntry, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.commitResultLocked(ctx, call, result)
}

func (e *Executor) commitResultLocked(ctx context.Context, call sessionentry.ToolCall, result tool.ToolResult) (sessionentry.ContextEntry, error) {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if stored, found, err := e.deps.Operations.GetResult(commitCtx, call.Key); err != nil || found {
		return stored, err
	}
	return e.deps.Operations.CommitResult(commitCtx, call, result)
}
