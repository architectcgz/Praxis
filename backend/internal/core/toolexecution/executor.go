package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"praxis/internal/clock"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	"praxis/internal/core/timing"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"strings"
	"sync"
)

type ExecutorDependencies struct {
	Reviewer       *Reviewer
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Clock          clock.Clock
	Logf           func(string, ...any)
	Outputs        OutputFiles
}

// OutputFiles 是工具结果的文件边界；临时目录解析与实际落盘由 Infra 提供。
type OutputFiles interface {
	Directory(identity.SessionID) string
	Bound(identity.SessionID, tool.ToolResult, int) (tool.ToolResult, error)
}

// Executor 只定位活跃 ToolCall；调用退出后立即释放审批和工具控制状态。
type Executor struct {
	deps   ExecutorDependencies
	mu     sync.Mutex
	active map[Key]*callState
}

type callState struct {
	call         sessionentry.ToolCall
	sourceTaskID identity.TaskID
	tool         *Execution
	request      *ApprovalRequest
	decision     *UserDecision
	review       *ReviewRecord
	stopped      bool
	wake         chan struct{}
}

func NewExecutor(deps ExecutorDependencies) *Executor {
	return &Executor{deps: deps, active: make(map[Key]*callState)}
}

// RunOwner 提供已绑定执行的只读事实，不依赖具体 Run 实现。
type RunOwner interface {
	BeginWaiting()
	EndWaiting()
	Main() bool
	ID() identity.RunID
	SessionID() identity.SessionID
	SourceTaskID() identity.TaskID
	Model() modelapi.Config
	InputText() string
	EventSource(identity.TurnID) coreevent.EventSource
}

// SaveModelRequest 在工具批次内同步提交审核请求事实，审核必须等到保存结果再继续。
type SaveModelRequest func(context.Context, sessionentry.ModelRequest) error

// RunTools 只绑定本次已取得运行权的 AgentRun 的环境。
type RunTools struct {
	run         RunOwner
	executor    *Executor
	projectPath string
	catalog     tool.ToolCatalog
	mode        security.ApprovalMode
	provider    providerapi.Provider
}

// CallResult 将实际结果和耗时交给调用方一起提交；仅执行路径携带 OperationID 和已准备参数。
// Err 只供后置观察，工具执行失败已编码在 ToolResult 中，不作为编排错误返回。
type CallResult struct {
	ToolResult  tool.ToolResult
	OperationID string
	Arguments   json.RawMessage
	Timing      *timing.Record
	Err         error
}

// ControlRequest 将审批和取消绑定到消息声明的归属。
type ControlRequest struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RunID     identity.RunID
	CallID    string
}

func (e *Executor) ForRun(run RunOwner, projectPath string, catalog tool.ToolCatalog, mode security.ApprovalMode, provider providerapi.Provider) *RunTools {
	return &RunTools{
		executor:    e,
		run:         run,
		projectPath: projectPath,
		catalog:     catalog,
		mode:        mode,
		provider:    provider,
	}
}

// Execute 冻结工具参数并取得最终授权；审核请求通过本批回调提交，工具回执由 Loop 保存。
func (tools *RunTools) Execute(ctx context.Context, call sessionentry.ToolCall, saveRequest SaveModelRequest) (CallResult, error) {
	s := tools.executor
	s.mu.Lock()
	s.active[call.Key] = &callState{
		call:         call,
		sourceTaskID: tools.run.SourceTaskID(),
		wake:         make(chan struct{}, 1),
	}
	s.mu.Unlock()
	defer s.unregister(call.Key)
	if err := ctx.Err(); err != nil {
		return callError("tool_interrupted", err.Error()), nil
	}
	t, ok := tools.catalog.Get(call.Name)
	if !ok {
		return callError("tool_not_allowed", "The requested tool is disabled or unregistered."), nil
	}
	operationID := hook.NewOperationID()
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
			*path = filepath.Join(tools.projectPath, *path)
		}
		*path = filepath.Clean(*path)
	}
	prepared, err := t.Prepare(arguments, tool.Environment{
		ProjectPath:     tools.projectPath,
		OutputDirectory: s.deps.Outputs.Directory(tools.run.SessionID()),
	})
	if err != nil {
		return callError("invalid_tool_call", err.Error()), nil
	}
	// 审批与 Hook 使用同一份只读 JSON，真实调用持有准备阶段绑定的参数，不反向解码。
	args, err := json.Marshal(prepared.Arguments)
	if err != nil {
		return CallResult{}, fmt.Errorf("encode prepared tool arguments: %w", err)
	}
	if err := s.deps.HookDispatcher.BeforeToolExecution(ctx, toolHookEvent(call, string(args), operationID)); err != nil {
		return CallResult{}, err
	}
	return tools.execute(ctx, call, prepared, args, operationID, saveRequest)
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

func (tools *RunTools) execute(ctx context.Context, call sessionentry.ToolCall, prepared tool.PreparedCall, args json.RawMessage, operationID string, saveRequest SaveModelRequest) (CallResult, error) {
	s := tools.executor
	denied, err := tools.authorize(ctx, call, args, operationID, saveRequest)
	if errors.Is(err, errCallStopped) {
		return callError("tool_interrupted", err.Error()), nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return callError("tool_interrupted", ctx.Err().Error()), nil
		}
		return CallResult{}, err
	}
	if denied != nil {
		return CallResult{ToolResult: *denied}, nil
	}

	observed := observedCall{
		call:    prepared.Call,
		name:    call.Name,
		run:     tools.run,
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
	execution := NewExecution(ctx, observed.Call)
	if err := s.register(ctx, call, execution); err != nil {
		execution.Stop()
		if errors.Is(err, errCallStopped) {
			return callError("tool_interrupted", err.Error()), nil
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
	bounded, outputErr := s.deps.Outputs.Bound(call.SessionID, value, tool.MaxInlineResultBytes)
	if outputErr != nil {
		sideEffect := value.SideEffect
		value = tool.NewToolError("tool_output_save_failed", "The tool has already run, but saving its output failed: "+outputErr.Error()+". Do not rerun the operation solely to recover its output.")
		value.SideEffect = sideEffect
		result.Err = errors.Join(result.Err, outputErr)
	} else {
		value = bounded
	}
	return CallResult{
		ToolResult:  value,
		OperationID: operationID,
		Arguments:   args,
		Timing:      observed.timing,
		Err:         result.Err,
	}, nil
}

// observedCall 只观察已绑定参数的真实调用，不参与授权、参数准备或结果保存。
type observedCall struct {
	call     func(context.Context) (tool.ToolResult, error)
	name     tool.ToolName
	timing   *timing.Record
	run      RunOwner
	hooks    *hook.Dispatcher
	emitter  *coreevent.Emitter
	identity hook.Identity
	callID   string
}

func (t *observedCall) Call(ctx context.Context) (result tool.ToolResult, err error) {
	ctx, scope := t.hooks.Start(ctx, hook.Operation{
		Identity:    t.identity,
		Kind:        hook.OperationTool,
		Name:        string(t.name),
		ReferenceID: t.callID,
	})
	defer func() { scope.End(hook.Result{Err: err, ToolErrorClass: result.ErrorClass}) }()
	span := timing.NewSpan()
	t.emitter.Emit(coreevent.Event{
		Source: coreevent.SourceForRun(t.run, t.identity.TurnID),
		Payload: coreevent.ToolStarted{
			CallID: t.callID,
			Timing: span.Snapshot(),
		},
	})
	span.Start()
	result, err = t.call(ctx)
	t.timing = span.End()
	return result, err
}

// toolHookEvent 仅在分发边界构造事件，参数以不可变字符串传递。
func toolHookEvent(call sessionentry.ToolCall, args string, operationID string) hook.ToolExecution {
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

func callError(code, message string) CallResult {
	return CallResult{ToolResult: tool.NewToolError(code, message)}
}

func (tools *RunTools) BoundResult(result tool.ToolResult, limit int) (tool.ToolResult, error) {
	return tools.executor.deps.Outputs.Bound(tools.run.SessionID(), result, limit)
}
