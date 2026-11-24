package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"praxis/internal/engine/hook"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/timing"
	"praxis/internal/tools"
	"strings"
	"sync"
)

var errCallStopped = errors.New("tool call was stopped")

type ExecutorDependencies struct {
	HookDispatcher    *hook.Dispatcher
	Emitter           *event.Emitter
	Outputs           OutputFiles
	InlineResultBytes int
}

// OutputFiles 由执行器消费；临时目录的布局和文件写入由实现方决定。
type OutputFiles interface {
	Directory(identity.SessionID) string
	Bound(identity.SessionID, tools.ToolResult, int) (tools.ToolResult, error)
}

// Authorizer 在工具执行前裁决授权，与工具准备、执行及结果限制解耦。
type Authorizer interface {
	Authorize(context.Context, entry.ToolCall, json.RawMessage, identity.OperationID, entry.AppendEntry) (*tools.ToolResult, error)
}

// RunOwner 提供工具执行事件和归属所需的最小只读视图。
type RunOwner interface {
	ID() identity.RunID
	SessionID() identity.SessionID
	EventSource(identity.TurnID) event.EventSource
}

// ControlRequest 将单工具停止命令绑定到活跃调用的归属。
type ControlRequest struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RunID     identity.RunID
	CallID    string
}

type activeCall struct {
	sessionID identity.SessionID
	agentID   identity.AgentID
	cancel    context.CancelCauseFunc
}

// Executor 负责活跃工具调用的取消句柄登记；调用退出后立即释放。
type Executor struct {
	deps   ExecutorDependencies
	mu     sync.Mutex
	active map[tools.CallKey]*activeCall
}

func NewExecutor(deps ExecutorDependencies) *Executor {
	return &Executor{
		deps:   deps,
		active: make(map[tools.CallKey]*activeCall),
	}
}

// RunTools 绑定单次 Run 的工具目录、项目范围与授权门控。
type RunTools struct {
	run         RunOwner
	executor    *Executor
	projectPath string
	catalog     tools.ToolCatalog
	authorizer  Authorizer
}

// CallResult 将实际结果和耗时交给调用方一起提交；仅执行路径携带 OperationID 和已准备参数。
// Err 只供后置观察，工具执行失败已编码在 ToolResult 中，不作为编排错误返回。
type CallResult struct {
	ToolResult  tools.ToolResult
	OperationID identity.OperationID
	Arguments   json.RawMessage
	Timing      *timing.Timing
	Err         error
}

func (e *Executor) ForRun(run RunOwner, projectPath string, catalog tools.ToolCatalog, authorizer Authorizer) *RunTools {
	return &RunTools{
		executor:    e,
		run:         run,
		projectPath: projectPath,
		catalog:     catalog,
		authorizer:  authorizer,
	}
}

func (e *Executor) register(key tools.CallKey, call *activeCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.active[key] = call
}

func (e *Executor) unregister(key tools.CallKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.active, key)
}

// StopCall 取消目标活跃工具调用的派生上下文，同时中断审核、审批等待或执行中的工具。
func (e *Executor) StopCall(ctx context.Context, request ControlRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	active := e.active[tools.CallKey{RunID: request.RunID, CallID: request.CallID}]
	if active == nil || active.sessionID != request.SessionID || active.agentID != request.AgentID {
		e.mu.Unlock()
		return errorcode.ErrRequestConflict
	}
	cancel := active.cancel
	e.mu.Unlock()
	cancel(errCallStopped)
	return nil
}

// Execute 冻结工具参数、按工具策略决定是否授权并执行调用；工具回执由 Loop 统一提交。
func (runTools *RunTools) Execute(ctx context.Context, call entry.ToolCall, appendEntry entry.AppendEntry) (CallResult, error) {
	s := runTools.executor
	callCtx, cancelCall := context.WithCancelCause(ctx)
	defer cancelCall(nil)

	s.register(call.CallKey, &activeCall{
		sessionID: call.SessionID,
		agentID:   call.AgentID,
		cancel:    cancelCall,
	})
	defer s.unregister(call.CallKey)

	if err := callCtx.Err(); err != nil {
		return callError("tool_interrupted", cancelReason(callCtx, err)), nil
	}
	t, ok := runTools.catalog.Get(call.Name)
	if !ok {
		return callError("tool_not_allowed", "The requested tool is disabled or unregistered."), nil
	}
	operationID := identity.NewOperationID()
	if err := callCtx.Err(); err != nil {
		return callError("tool_interrupted", cancelReason(callCtx, err)), nil
	}

	// JSON 只在编排边界解码；工具准备与实际执行都使用类型化参数。
	arguments := t.NewArguments()
	var path *string
	var defaultPath string
	if value, ok := arguments.(tools.PathArguments); ok {
		path = value.PathArgument()
		// 解码前保留业务默认路径，显式空值仍按该工具自己的缺省规则处理。
		defaultPath = *path
	}
	if err := decodeToolArguments(call.Arguments, arguments); err != nil {
		return callError("invalid_tool_call", err.Error()), nil
	}
	// 项目根目录已由业务入口校验；模型路径只在此处解析，准备与授权均使用规范化后的同一值。
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
			*path = filepath.Join(runTools.projectPath, *path)
		}
		*path = filepath.Clean(*path)
	}
	prepared, err := t.Prepare(arguments, tools.Environment{
		ProjectPath:     runTools.projectPath,
		OutputDirectory: s.deps.Outputs.Directory(runTools.run.SessionID()),
		CallID:          call.CallID,
	})
	if err != nil {
		return callError("invalid_tool_call", err.Error()), nil
	}
	// 审批与 Hook 使用同一份只读 JSON，真实调用持有准备阶段绑定的参数，不反向解码。
	args, err := json.Marshal(prepared.Arguments)
	if err != nil {
		return CallResult{}, fmt.Errorf("encode prepared tool arguments: %w", err)
	}
	if err := s.deps.HookDispatcher.BeforeToolExecution(callCtx, toolHookEvent(call, string(args), operationID)); err != nil {
		return CallResult{}, err
	}
	if tools.RequiresApproval(t) && runTools.authorizer != nil {
		denied, authErr := runTools.authorizer.Authorize(callCtx, call, args, operationID, appendEntry)
		if errors.Is(context.Cause(callCtx), errCallStopped) {
			return callError("tool_interrupted", errCallStopped.Error()), nil
		}
		if authErr != nil {
			if callCtx.Err() != nil && !errors.Is(authErr, errorcode.ErrStorage) {
				return callError("tool_interrupted", cancelReason(callCtx, authErr)), nil
			}
			return CallResult{}, authErr
		}
		if denied != nil {
			return CallResult{ToolResult: *denied}, nil
		}
	}
	if err := callCtx.Err(); err != nil {
		return callError("tool_interrupted", cancelReason(callCtx, err)), nil
	}

	observed := observedCall{
		call:    prepared.Call,
		name:    call.Name,
		run:     runTools.run,
		hooks:   s.deps.HookDispatcher,
		emitter: s.deps.Emitter,
		identity: hook.Identity{
			OperationID: operationID,
			SessionID:   call.SessionID,
			AgentID:     call.AgentID,
			RunID:       call.RunID,
			TurnID:      call.TurnID,
		},
		callID: call.CallID,
	}
	value, execErr := observed.Call(callCtx)
	if errors.Is(execErr, errorcode.ErrStorage) {
		return CallResult{}, execErr
	}
	if execErr != nil {
		if value.Payload == "" {
			code := "tool_execution_failed"
			message := execErr.Error()
			if errors.Is(context.Cause(callCtx), errCallStopped) {
				code = "tool_interrupted"
				message = errCallStopped.Error()
			} else if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
				code = "tool_interrupted"
			}
			value = tools.NewToolError(code, message)
		} else if value.ErrorClass == "" {
			value.ErrorClass = "tool_execution_failed"
		}
	}
	bounded, outputErr := s.deps.Outputs.Bound(call.SessionID, value, s.deps.InlineResultBytes)
	if outputErr != nil {
		sideEffect := value.SideEffect
		value = tools.NewToolError("tool_output_save_failed", "The tool has already run, but saving its output failed: "+outputErr.Error()+". Do not rerun the operation solely to recover its output.")
		value.SideEffect = sideEffect
		execErr = errors.Join(execErr, outputErr)
	} else {
		value = bounded
	}
	return CallResult{
		ToolResult:  value,
		OperationID: operationID,
		Arguments:   args,
		Timing:      observed.timing,
		Err:         execErr,
	}, nil
}

// decodeToolArguments 只接受单个 JSON 对象，不修复原文，也不忽略对象后的多余内容。
func decodeToolArguments(raw string, target any) error {
	input := strings.TrimSpace(raw)
	if len(input) == 0 || input[0] != '{' {
		return errors.New("tool arguments must be a JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("tool arguments must contain a single JSON object")
	}
	return nil
}

// observedCall 只观察已绑定参数的真实调用，不参与授权、参数准备或结果保存。
type observedCall struct {
	call     func(context.Context) (tools.ToolResult, error)
	name     tools.ToolName
	timing   *timing.Timing
	run      RunOwner
	hooks    *hook.Dispatcher
	emitter  *event.Emitter
	identity hook.Identity
	callID   string
}

func (t *observedCall) Call(ctx context.Context) (result tools.ToolResult, err error) {
	ctx, scope := t.hooks.Start(ctx, hook.Operation{
		Identity:    t.identity,
		Kind:        hook.OperationTool,
		Name:        string(t.name),
		ReferenceID: t.callID,
	})
	defer func() { scope.End(hook.Result{Err: err, ToolErrorClass: result.ErrorClass}) }()
	span := timing.NewSpan()
	t.emitter.Emit(event.Event{
		Source: t.run.EventSource(t.identity.TurnID),
		Payload: event.ToolStarted{
			CallID: t.callID,
			Timing: span.Snapshot(),
		},
	})
	span.Start()
	result, err = t.call(ctx)
	t.timing = span.End()
	return result, err
}

func toolHookEvent(call entry.ToolCall, args string, operationID identity.OperationID) hook.ToolExecution {
	return hook.ToolExecution{
		Identity: hook.Identity{
			OperationID: operationID,
			SessionID:   call.SessionID,
			AgentID:     call.AgentID,
			RunID:       call.RunID,
			TurnID:      call.TurnID,
		},
		Call:      call,
		Arguments: args,
	}
}

func cancelReason(ctx context.Context, fallback error) string {
	if cause := context.Cause(ctx); cause != nil {
		return cause.Error()
	}
	return fallback.Error()
}

func callError(code, message string) CallResult {
	return CallResult{ToolResult: tools.NewToolError(code, message)}
}

func (runTools *RunTools) SpillResult(result tools.ToolResult) (tools.ToolResult, error) {
	return runTools.executor.deps.Outputs.Bound(runTools.run.SessionID(), result, 0)
}
