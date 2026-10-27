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

// CallResult 将实际结果交给 Loop 保存；仅执行路径携带 OperationID 和已准备参数。
// Err 只供后置观察，工具执行失败已编码在 ToolResult 中，不作为编排错误返回。
type CallResult struct {
	ToolResult  tool.ToolResult
	OperationID string
	Arguments   json.RawMessage
	Err         error
}

func (e *Executor) ForExecution(run *agentexecution.Run, workspacePath string, awaitApproval func(context.Context) error) *ExecutionTools {
	return &ExecutionTools{
		executor:      e,
		run:           run,
		workspacePath: workspacePath,
		awaitApproval: awaitApproval,
	}
}

// Execute 准备并冻结工具参数，取得最终授权后返回实际结果；不写历史或调用后置 Hook。
func (tools *ExecutionTools) Execute(ctx context.Context, call sessionentry.ToolCall) (CallResult, error) {
	s := tools.executor
	owner := tools.run.Snapshot()
	s.mu.Lock()
	if s.active[call.Key] != nil {
		s.mu.Unlock()
		return CallResult{}, errorcode.ErrRequestConflict
	}
	s.active[call.Key] = &callState{call: call, owner: owner, run: tools.run}
	s.mu.Unlock()
	defer s.unregister(call.Key)
	if stored, found := tools.run.Result(call.Key); found {
		return existingResult(stored), nil
	}
	if err := ctx.Err(); err != nil {
		return callError("tool_interrupted", err.Error()), nil
	}
	t, ok := s.deps.Catalog.Get(call.Name)
	if !ok {
		return callError("tool_not_allowed", "The requested tool is disabled or unregistered."), nil
	}
	operationID := hook.NewOperationID()
	modified, hookErr := s.deps.HookDispatcher.BeforeToolExecution(ctx, toolHookEvent(call, call.Arguments, operationID))
	if hookErr != nil {
		return CallResult{}, hookErr
	}
	if err := ctx.Err(); err != nil {
		return callError("tool_interrupted", err.Error()), nil
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
		return callError("invalid_tool_call", err.Error()), nil
	}
	// 工作区根已由业务入口校验；模型路径只在此处解析，准备与授权均使用规范化后的同一值。
	if path != nil {
		if strings.TrimSpace(*path) == "" {
			*path = defaultPath
		}
		if strings.TrimSpace(*path) == "" {
			return callError("invalid_tool_call", "path is required"), nil
		}
		if strings.ContainsAny(*path, "\x00\r\n") {
			return callError("invalid_tool_call", "path contains control characters"), nil
		}
		if !filepath.IsAbs(*path) {
			*path = filepath.Join(tools.workspacePath, *path)
		}
		*path = filepath.Clean(*path)
	}
	prepared, err := t.Prepare(arguments, tools.workspacePath)
	if err != nil {
		return callError("invalid_tool_call", err.Error()), nil
	}
	// 审批与 Hook 使用同一份只读 JSON，真实调用持有准备阶段绑定的参数，不反向解码。
	args, err := json.Marshal(prepared.Arguments)
	if err != nil {
		return CallResult{}, fmt.Errorf("encode prepared tool arguments: %w", err)
	}
	return tools.execute(ctx, call, prepared, args, operationID)
}

func (tools *ExecutionTools) execute(ctx context.Context, call sessionentry.ToolCall, prepared tool.PreparedCall, args json.RawMessage, operationID string) (CallResult, error) {
	s := tools.executor

	for {
		if stored, found := tools.run.Result(call.Key); found {
			return existingResult(stored), nil
		}
		denied, err := tools.authorize(ctx, call, args, prepared.Command, operationID)
		if errors.Is(err, errCallStopped) {
			return callError("tool_interrupted", err.Error()), nil
		}
		if err != nil {
			if errors.Is(err, errResultAvailable) {
				continue
			}
			if ctx.Err() != nil {
				return callError("tool_interrupted", ctx.Err().Error()), nil
			}
			return CallResult{}, err
		}
		if denied != nil {
			return CallResult{ToolResult: *denied}, nil
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
				return callError("tool_interrupted", err.Error()), nil
			}
			if errors.Is(err, errResultAvailable) {
				continue
			}
			if ctx.Err() != nil {
				return callError("tool_interrupted", ctx.Err().Error()), nil
			}
			return CallResult{}, err
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
		return CallResult{
			ToolResult:  value,
			OperationID: operationID,
			Arguments:   args,
			Err:         result.Err,
		}, nil
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

func callError(code, message string) CallResult {
	return CallResult{ToolResult: tool.NewToolError(code, message)}
}

// existingResult 只复用已接纳回执，不重复执行工具或后置 Hook。
func existingResult(entry sessionentry.ContextEntry) CallResult {
	block := entry.Content[0]
	result := tool.ToolResult{Payload: block.Text}
	if block.IsError {
		result.ErrorClass = "tool_execution_failed"
	}
	return CallResult{ToolResult: result}
}
