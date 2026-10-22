package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"reflect"
	"slices"
)

// Dispatcher 冻结可选业务接口的注册顺序；回调同步执行，共享处理器应支持并发 Execution。
// 转换回调返回下一处理器的输入；通知回调的错误由业务调用方记录。
type Dispatcher struct {
	hooks []any
}

// NewDispatcher 只接受至少实现一个业务 Hook 接口的处理器，不要求实现全部切入点。
func NewDispatcher(hooks ...any) (*Dispatcher, error) {
	for _, registered := range hooks {
		if registered == nil {
			return nil, errors.New("hook registration is nil")
		}
		value := reflect.ValueOf(registered)
		if (value.Kind() == reflect.Pointer || value.Kind() == reflect.Func || value.Kind() == reflect.Interface || value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.Chan) && value.IsNil() {
			return nil, errors.New("hook registration is nil")
		}
		switch registered.(type) {
		case OperationHook,
			BeforeUserInputHook,
			BeforeContextBuildHook,
			BeforeModelRequestHook,
			BeforeToolExecutionHook,
			AfterToolExecutionHook,
			UserInputCommittedHook,
			AssistantCommittedHook,
			FinalOutputCommittedHook,
			ExecutionStartedHook,
			ExecutionResumedHook,
			ExecutionPausedHook,
			ExecutionCommittedHook,
			AfterContextBuildHook,
			BeforeContextCompressionHook,
			AfterContextCompressionHook,
			BeforeIterationHook,
			AfterIterationHook,
			AfterModelResponseHook,
			ToolExecutionWaitingHook:
		default:
			return nil, fmt.Errorf("unsupported hook registration %T", registered)
		}
	}
	return &Dispatcher{hooks: slices.Clone(hooks)}, nil
}

// WithHooks 返回独立注册表，不修改正在使用的 Dispatcher。
func (d *Dispatcher) WithHooks(hooks ...any) (*Dispatcher, error) {
	var registered []any
	if d != nil {
		registered = slices.Clone(d.hooks)
	}
	return NewDispatcher(append(registered, hooks...)...)
}

// registered 在无扩展时返回空列表，执行正确性不依赖 Hook 是否装配。
func (d *Dispatcher) registered() []any {
	if d == nil {
		return nil
	}
	return d.hooks
}

func (d *Dispatcher) BeforeUserInput(ctx context.Context, event UserInput) (string, error) {
	value := event.Prompt
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeUserInputHook)
		if !ok {
			continue
		}
		next, err := handler.BeforeUserInput(ctx, event)
		if err != nil {
			var empty string
			return empty, &Error{Point: "BeforeUserInput", Cause: err}
		}
		value = next
		event.Prompt = next
	}
	return value, nil
}

func (d *Dispatcher) BeforeContextBuild(ctx context.Context, event ContextBuild) (appcontext.Content, error) {
	value := event.Content
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeContextBuildHook)
		if !ok {
			continue
		}
		next, err := handler.BeforeContextBuild(ctx, event.clone())
		if err != nil {
			var empty appcontext.Content
			return empty, &Error{Point: "BeforeContextBuild", Cause: err}
		}
		value = cloneContent(next)
		event.Content = value
	}
	return value, nil
}

func (d *Dispatcher) BeforeModelRequest(ctx context.Context, event ModelRequest) (providerapi.Request, error) {
	value := event.Request
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeModelRequestHook)
		if !ok {
			continue
		}
		next, err := handler.BeforeModelRequest(ctx, event.clone())
		if err != nil {
			var empty providerapi.Request
			return empty, &Error{Point: "BeforeModelRequest", Cause: err}
		}
		value = next.Clone()
		event.Request = value
	}
	return value, nil
}

func (d *Dispatcher) BeforeToolExecution(ctx context.Context, event ToolExecution) (json.RawMessage, error) {
	value := bytes.Clone(event.Arguments)
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeToolExecutionHook)
		if !ok {
			continue
		}
		next, err := handler.BeforeToolExecution(ctx, event.clone())
		if err != nil {
			var empty json.RawMessage
			return empty, &Error{Point: "BeforeToolExecution", Cause: err}
		}
		value = bytes.Clone(next)
		event.Arguments = value
	}
	return value, nil
}

func (d *Dispatcher) AfterToolExecution(ctx context.Context, event ToolExecutionResult) (tool.ToolResult, error) {
	value := event.Result
	for _, registered := range d.registered() {
		handler, ok := registered.(AfterToolExecutionHook)
		if !ok {
			continue
		}
		next, err := handler.AfterToolExecution(ctx, event.clone())
		if err != nil {
			var empty tool.ToolResult
			return empty, &Error{Point: "AfterToolExecution", Cause: err}
		}
		value = next
		event.Result = next
	}
	return value, nil
}

func (d *Dispatcher) UserInputCommitted(ctx context.Context, event EntryCommitted) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(UserInputCommittedHook)
		if !ok {
			continue
		}
		if err := handler.UserInputCommitted(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "UserInputCommitted", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) AssistantCommitted(ctx context.Context, event EntryCommitted) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(AssistantCommittedHook)
		if !ok {
			continue
		}
		if err := handler.AssistantCommitted(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "AssistantCommitted", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) FinalOutputCommitted(ctx context.Context, event EntryCommitted) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(FinalOutputCommittedHook)
		if !ok {
			continue
		}
		if err := handler.FinalOutputCommitted(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "FinalOutputCommitted", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) ExecutionStarted(ctx context.Context, event ExecutionEvent) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(ExecutionStartedHook)
		if !ok {
			continue
		}
		if err := handler.ExecutionStarted(ctx, event); err != nil {
			errs = append(errs, &Error{Point: "ExecutionStarted", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) ExecutionResumed(ctx context.Context, event ExecutionEvent) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(ExecutionResumedHook)
		if !ok {
			continue
		}
		if err := handler.ExecutionResumed(ctx, event); err != nil {
			errs = append(errs, &Error{Point: "ExecutionResumed", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) ExecutionPaused(ctx context.Context, event ExecutionEvent) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(ExecutionPausedHook)
		if !ok {
			continue
		}
		if err := handler.ExecutionPaused(ctx, event); err != nil {
			errs = append(errs, &Error{Point: "ExecutionPaused", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) ExecutionCommitted(ctx context.Context, event ExecutionEvent) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(ExecutionCommittedHook)
		if !ok {
			continue
		}
		if err := handler.ExecutionCommitted(ctx, event); err != nil {
			errs = append(errs, &Error{Point: "ExecutionCommitted", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) AfterContextBuild(ctx context.Context, event ContextBuilt) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(AfterContextBuildHook)
		if !ok {
			continue
		}
		if err := handler.AfterContextBuild(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "AfterContextBuild", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) BeforeContextCompression(ctx context.Context, event ContextCompression) error {
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeContextCompressionHook)
		if !ok {
			continue
		}
		if err := handler.BeforeContextCompression(ctx, event.clone()); err != nil {
			return &Error{Point: "BeforeContextCompression", Cause: err}
		}
	}
	return nil
}

func (d *Dispatcher) AfterContextCompression(ctx context.Context, event ContextCompressionResult) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(AfterContextCompressionHook)
		if !ok {
			continue
		}
		if err := handler.AfterContextCompression(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "AfterContextCompression", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) BeforeIteration(ctx context.Context, event Iteration) error {
	for _, registered := range d.registered() {
		handler, ok := registered.(BeforeIterationHook)
		if !ok {
			continue
		}
		if err := handler.BeforeIteration(ctx, event); err != nil {
			return &Error{Point: "BeforeIteration", Cause: err}
		}
	}
	return nil
}

func (d *Dispatcher) AfterIteration(ctx context.Context, event IterationResult) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(AfterIterationHook)
		if !ok {
			continue
		}
		if err := handler.AfterIteration(ctx, event); err != nil {
			errs = append(errs, &Error{Point: "AfterIteration", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) AfterModelResponse(ctx context.Context, event ModelResponse) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(AfterModelResponseHook)
		if !ok {
			continue
		}
		if err := handler.AfterModelResponse(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "AfterModelResponse", Cause: err})
		}
	}
	return errors.Join(errs...)
}

func (d *Dispatcher) ToolExecutionWaiting(ctx context.Context, event ToolExecution) error {
	var errs []error
	for _, registered := range d.registered() {
		handler, ok := registered.(ToolExecutionWaitingHook)
		if !ok {
			continue
		}
		if err := handler.ToolExecutionWaiting(ctx, event.clone()); err != nil {
			errs = append(errs, &Error{Point: "ToolExecutionWaiting", Cause: err})
		}
	}
	return errors.Join(errs...)
}

// Start 逐级传播 Context；结束时逆序关闭各操作 Scope。
func (d *Dispatcher) Start(ctx context.Context, operation Operation) (context.Context, Scope) {
	if operation.OperationID == "" {
		operation.OperationID = NewOperationID()
	}
	var scopes []Scope
	for _, registered := range d.registered() {
		handler, ok := registered.(OperationHook)
		if !ok {
			continue
		}
		nextCtx, scope := handler.Start(ctx, operation)
		ctx = nextCtx
		if scope != nil {
			scopes = append(scopes, scope)
		}
	}
	return ctx, compositeScope{scopes: scopes}
}

type compositeScope struct{ scopes []Scope }

func (s compositeScope) OnSignal(signal Signal) {
	for _, scope := range s.scopes {
		scope.OnSignal(signal)
	}
}
func (s compositeScope) End(result Result) {
	for i := len(s.scopes) - 1; i >= 0; i-- {
		s.scopes[i].End(result)
	}
}
