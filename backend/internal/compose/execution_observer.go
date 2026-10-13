package compose

import (
	"context"
	"errors"
	"sync"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
)

// executionObserver 在组合根把执行事件转换为 Hook 生命周期；业务模块只发布执行事实。
type executionObserver struct {
	dispatcher *hooks.Dispatcher

	mu        sync.Mutex
	agents    map[contracts.TaskID]context.Context
	agentSpan map[contracts.TaskID]hooks.Scope
	providers map[operationKey]hooks.Scope
	tools     map[toolOperationKey]hooks.Scope
}

type operationKey struct {
	taskID contracts.TaskID
	turnID contracts.TurnID
}

type toolOperationKey struct {
	operationKey
	callID string
}

func newExecutionObserver(dispatcher *hooks.Dispatcher) *executionObserver {
	return &executionObserver{
		dispatcher: dispatcher,
		agents:     make(map[contracts.TaskID]context.Context),
		agentSpan:  make(map[contracts.TaskID]hooks.Scope),
		providers:  make(map[operationKey]hooks.Scope),
		tools:      make(map[toolOperationKey]hooks.Scope),
	}
}

// StartAgent 在组合层建立 Agent Hook，并保存其 Context 作为子操作的父 Context。
func (o *executionObserver) StartAgent(ctx context.Context, task taskmodel.Task) (context.Context, hooks.Scope) {
	if o == nil {
		return ctx, hooks.Scope(nil)
	}
	runCtx, span := o.dispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			TaskID:    task.ID,
		},
		Kind: hooks.OperationAgent,
		Name: task.Input.AgentDefinitionID.String(),
	})
	o.mu.Lock()
	o.agents[task.ID] = runCtx
	o.agentSpan[task.ID] = span
	o.mu.Unlock()
	return runCtx, span
}

// EndAgent 先结束未收到完成事件的子操作，再结束 Agent Hook，避免留下 running 记录。
func (o *executionObserver) EndAgent(taskID contracts.TaskID, result hooks.Result) {
	if o == nil {
		return
	}
	o.mu.Lock()
	delete(o.agents, taskID)
	span := o.agentSpan[taskID]
	delete(o.agentSpan, taskID)
	var children []hooks.Scope
	for key, provider := range o.providers {
		if key.taskID == taskID {
			children = append(children, provider)
			delete(o.providers, key)
		}
	}
	for key, tool := range o.tools {
		if key.taskID == taskID {
			children = append(children, tool)
			delete(o.tools, key)
		}
	}
	o.mu.Unlock()
	for _, child := range children {
		child.End(result)
	}
	if span != nil {
		span.End(result)
	}
}

// Observe 接收业务执行事件并在组合层推进 Provider、Tool 的 Hook 生命周期。
func (o *executionObserver) Observe(event agentruntime.AgentEvent) {
	if o == nil {
		return
	}
	switch event.Kind {
	case agentruntime.AgentEventProviderWaiting:
		o.startProvider(event)
	case agentruntime.AgentEventTurnCompleted:
		o.endProvider(event, hooks.Result{StopReason: event.StopReason})
	case agentruntime.AgentEventToolCall:
		o.startTool(event)
	case agentruntime.AgentEventToolResult:
		result := hooks.Result{}
		if event.IsError {
			result.Err = errors.New("tool execution failed")
		}
		o.endTool(event, result)
	case agentruntime.AgentEventError:
		o.endProvider(event, hooks.Result{Err: errors.New(event.Error)})
		o.endTools(event, hooks.Result{Err: errors.New(event.Error)})
	}
}

func (o *executionObserver) startProvider(event agentruntime.AgentEvent) {
	key := operationKey{taskID: event.TaskID, turnID: event.TurnID}
	o.mu.Lock()
	if _, exists := o.providers[key]; exists {
		o.mu.Unlock()
		return
	}
	ctx := o.agents[event.TaskID]
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := o.dispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: event.SessionID,
			AgentID:   event.AgentID,
			TaskID:    event.TaskID,
			TurnID:    event.TurnID,
		},
		Kind:        hooks.OperationProvider,
		Name:        event.Name,
		ReferenceID: "assistant:" + event.TurnID.String(),
	})
	if span != nil {
		o.providers[key] = span
	}
	o.mu.Unlock()
}

func (o *executionObserver) endProvider(event agentruntime.AgentEvent, result hooks.Result) {
	key := operationKey{taskID: event.TaskID, turnID: event.TurnID}
	o.mu.Lock()
	span := o.providers[key]
	delete(o.providers, key)
	o.mu.Unlock()
	if span != nil {
		span.End(result)
	}
}

func (o *executionObserver) startTool(event agentruntime.AgentEvent) {
	key := toolOperationKey{operationKey: operationKey{taskID: event.TaskID, turnID: event.TurnID}, callID: event.CallID}
	o.mu.Lock()
	if _, exists := o.tools[key]; exists {
		o.mu.Unlock()
		return
	}
	ctx := o.agents[event.TaskID]
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := o.dispatcher.Start(ctx, hooks.Operation{
		Identity: hooks.Identity{
			SessionID: event.SessionID,
			AgentID:   event.AgentID,
			TaskID:    event.TaskID,
			TurnID:    event.TurnID,
		},
		Kind:        hooks.OperationTool,
		Name:        event.Name,
		ReferenceID: event.CallID,
	})
	if span != nil {
		o.tools[key] = span
	}
	o.mu.Unlock()
}

func (o *executionObserver) endTool(event agentruntime.AgentEvent, result hooks.Result) {
	key := toolOperationKey{operationKey: operationKey{taskID: event.TaskID, turnID: event.TurnID}, callID: event.CallID}
	o.mu.Lock()
	span := o.tools[key]
	delete(o.tools, key)
	o.mu.Unlock()
	if span != nil {
		span.End(result)
	}
}

func (o *executionObserver) endTools(event agentruntime.AgentEvent, result hooks.Result) {
	o.mu.Lock()
	var spans []hooks.Scope
	for key, span := range o.tools {
		if key.taskID == event.TaskID && (event.TurnID == "" || key.turnID == event.TurnID) {
			spans = append(spans, span)
			delete(o.tools, key)
		}
	}
	o.mu.Unlock()
	for _, span := range spans {
		span.End(result)
	}
}

// observedLoop 在组合层建立 Agent Hook；loop 本身不感知观测机制。
func observedLoop(next agentruntime.LoopFunc, observer *executionObserver) agentruntime.LoopFunc {
	return func(ctx context.Context, task taskmodel.Task, messageRecorder agentruntime.MessageRecorder) (outcome taskmodel.TaskOutcome, code contracts.TaskFailureCode, err error) {
		if observer == nil {
			return next(ctx, task, messageRecorder)
		}
		runCtx, _ := observer.StartAgent(ctx, task)
		defer observer.EndAgent(task.ID, hooks.Result{Outcome: outcome, Err: err})
		return next(runCtx, task, messageRecorder)
	}
}
