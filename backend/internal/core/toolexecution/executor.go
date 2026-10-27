package toolexecution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"praxis/internal/clock"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"strings"
	"sync"
	"time"
)

type ExecutorDependencies struct {
	Catalog        tool.ToolCatalog
	Operations     Operations
	Reviewer       *Reviewer
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	Logf           func(string, ...any)
}

// Executor 只定位活跃 ToolCall；调用退出后立即释放审批和工具控制状态。
type Executor struct {
	deps   ExecutorDependencies
	mu     sync.Mutex
	active map[Key]*callState
}

type callState struct {
	run      *agentexecution.Run
	call     sessionentry.ToolCall
	owner    agentexecution.ExecutionRecord
	tool     *Execution
	request  *ApprovalRequest
	decision *UserDecision
	review   *ReviewRecord
	stopped  bool
}

func NewExecutor(deps ExecutorDependencies) *Executor {
	return &Executor{deps: deps, active: make(map[Key]*callState)}
}

// ExecutionTools 只绑定本次已取得运行权的 AgentExecution 的环境。
type ExecutionTools struct {
	run           *agentexecution.Run
	executor      *Executor
	workspacePath string
	awaitApproval func(context.Context) error
}

func (e *Executor) ForExecution(run *agentexecution.Run, workspacePath string, awaitApproval func(context.Context) error) *ExecutionTools {
	return &ExecutionTools{
		executor:      e,
		run:           run,
		workspacePath: workspacePath,
		awaitApproval: awaitApproval,
	}
}

// Execute 准备并冻结工具参数，取得最终授权后执行工具并提交回执；审核与审批等待由 authorize 调度。
func (tools *ExecutionTools) Execute(ctx context.Context, call sessionentry.ToolCall) (sessionentry.ContextEntry, error) {
	s := tools.executor
	owner := tools.run.Snapshot()
	s.mu.Lock()
	if s.active[call.Key] != nil {
		s.mu.Unlock()
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	s.active[call.Key] = &callState{call: call, owner: owner, run: tools.run}
	s.mu.Unlock()
	defer s.unregister(call.Key)
	if stored, found := tools.run.Result(call.Key); found {
		return stored, nil
	}
	if err := ctx.Err(); err != nil {
		return s.appendError(ctx, call, "tool_interrupted", err.Error())
	}
	t, ok := s.deps.Catalog.Get(call.Name)
	if !ok {
		return s.appendError(ctx, call, "tool_not_allowed", "The requested tool is disabled or unregistered.")
	}
	operationID := hook.NewOperationID()
	modified, hookErr := s.deps.HookDispatcher.BeforeToolExecution(ctx, toolHookEvent(call, call.Arguments, operationID))
	if hookErr != nil {
		return sessionentry.ContextEntry{}, hookErr
	}
	if err := ctx.Err(); err != nil {
		return s.appendError(ctx, call, "tool_interrupted", err.Error())
	}
	// JSON 只在编排边界解码；工具准备与实际执行都使用类型化参数。
	arguments := t.NewArguments()
	var path *string
	var defaultPath string
	if value, ok := arguments.(tool.PathArguments); ok {
		path = value.PathArgument()
		// 解码前保留业务默认路径，显式空值仍按该工具自己的缺省规则处理。
		defaultPath = *path
	}
	decoder := json.NewDecoder(bytes.NewReader(modified))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(arguments); err != nil {
		return s.appendError(ctx, call, "invalid_tool_call", err.Error())
	}
	// 工作区根已由业务入口校验；模型路径只在此处解析，准备与授权均使用规范化后的同一值。
	if path != nil {
		if strings.TrimSpace(*path) == "" {
			*path = defaultPath
		}
		if strings.TrimSpace(*path) == "" {
			return s.appendError(ctx, call, "invalid_tool_call", "path is required")
		}
		if strings.ContainsAny(*path, "\x00\r\n") {
			return s.appendError(ctx, call, "invalid_tool_call", "path contains control characters")
		}
		if !filepath.IsAbs(*path) {
			*path = filepath.Join(tools.workspacePath, *path)
		}
		*path = filepath.Clean(*path)
	}
	prepared, err := t.Prepare(arguments, tools.workspacePath)
	if err != nil {
		return s.appendError(ctx, call, "invalid_tool_call", err.Error())
	}
	// 审批与 Hook 使用同一份只读 JSON，真实调用持有准备阶段绑定的参数，不反向解码。
	args, err := json.Marshal(prepared.Arguments)
	if err != nil {
		return sessionentry.ContextEntry{}, fmt.Errorf("encode prepared tool arguments: %w", err)
	}
	return tools.execute(ctx, call, prepared, args, operationID)
}

func (tools *ExecutionTools) execute(ctx context.Context, call sessionentry.ToolCall, prepared tool.PreparedCall, args json.RawMessage, operationID string) (sessionentry.ContextEntry, error) {
	s := tools.executor

	for {
		if stored, found := tools.run.Result(call.Key); found {
			return stored, nil
		}
		denied, err := tools.authorize(ctx, call, args, prepared.Command, operationID)
		if errors.Is(err, errCallStopped) {
			return s.appendError(ctx, call, "tool_interrupted", err.Error())
		}
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

		observed := observedCall{
			call:     prepared.Call,
			name:     call.Name,
			recorder: tools.run.Timing,
			hooks:    s.deps.HookDispatcher,
			identity: hook.Identity{
				OperationID: operationID,
				SessionID:   call.SessionID,
				AgentID:     call.AgentID,
				ExecutionID: call.ExecutionID,
				TurnID:      call.TurnID,
			},
			callID: call.CallID,
		}
		execution := NewExecution(ctx, observed.Call)
		err = s.register(ctx, call, execution)
		if err != nil {
			execution.Stop()
			if errors.Is(err, errCallStopped) {
				return s.appendError(ctx, call, "tool_interrupted", err.Error())
			}
			if errors.Is(err, errResultAvailable) {
				continue
			}
			if ctx.Err() != nil {
				return s.appendError(ctx, call, "tool_interrupted", ctx.Err().Error())
			}

			return sessionentry.ContextEntry{}, err
		}
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
			ToolExecution: toolHookEvent(call, args, operationID),
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

// observedCall 只观察已绑定参数的真实调用，不参与授权、参数准备或结果保存。
type observedCall struct {
	call     func(context.Context) (tool.ToolResult, error)
	name     tool.ToolName
	recorder *timing.Recorder
	hooks    *hook.Dispatcher
	identity hook.Identity
	callID   string
}

func (t observedCall) Call(ctx context.Context) (result tool.ToolResult, err error) {
	ctx, scope := t.hooks.Start(ctx, hook.Operation{
		Identity:    t.identity,
		Kind:        hook.OperationTool,
		Name:        string(t.name),
		ReferenceID: t.callID,
	})
	defer func() { scope.End(hook.Result{Err: err, ToolErrorClass: result.ErrorClass}) }()
	ctx, span := t.recorder.Start(ctx, timing.Operation{
		ExecutionID: t.identity.ExecutionID.String(),
		Kind:        timing.Tool,
		Name:        string(t.name),
		ReferenceID: t.callID,
	})
	result, err = t.call(ctx)
	status := timing.ResultStatus(ctx, err)
	if status == timing.Completed && result.ErrorClass != "" {
		status = timing.Failed
	}
	span.End(status)
	return result, err
}

// toolHookEvent 仅在分发边界构造事件，参数隔离由 HookDispatcher 负责。
func toolHookEvent(call sessionentry.ToolCall, args json.RawMessage, operationID string) hook.ToolExecution {
	return hook.ToolExecution{
		Identity: hook.Identity{
			OperationID: operationID,
			SessionID:   call.SessionID,
			AgentID:     call.AgentID,
			ExecutionID: call.ExecutionID,
			TurnID:      call.TurnID,
		},
		Call:      call,
		Arguments: args,
	}
}

func (e *Executor) appendError(ctx context.Context, call sessionentry.ToolCall, code, message string) (sessionentry.ContextEntry, error) {
	return e.commitResult(ctx, call, tool.NewToolError(code, message))
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
	callState := e.active[call.Key]
	if callState == nil {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	if stored, found := callState.run.Result(call.Key); found {
		return stored, nil
	}
	return e.deps.Operations.CommitResult(commitCtx, callState.run, call, result)
}
