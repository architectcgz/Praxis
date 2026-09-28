package agent

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"errors"
	"fmt"
	"sync"

	runtimecontract "praxis/internal/runtime"
)

// ManagedRuntime 只暴露进程内生命周期操作。
// 持久化产品状态仍由 execution application service 负责。
type ManagedRuntime interface {
	Activate(context.Context, executionmodel.AgentExecution, runtimecontract.ExecutionLifecycle) error
	Cancel(context.Context, contracts.AgentExecutionID, executionmodel.ExecutionOutcome) (bool, error)
	Close(context.Context) error
}

// ManagedRuntimeFactory 在首次使用时为 Agent 创建 runtime。
type ManagedRuntimeFactory interface {
	New(context.Context, contracts.AgentID) (ManagedRuntime, error)
}

// Registry 管理每个 Agent 的进程内 runtime。
// 产品所有权仍以 SQLite 的活动 execution 约束为准。
type Registry struct {
	factory ManagedRuntimeFactory

	mu      sync.Mutex
	entries map[contracts.AgentID]ManagedRuntime
	closed  bool
}

// NewRegistry 使用给定 factory 创建空注册表。
func NewRegistry(factory ManagedRuntimeFactory) (*Registry, error) {
	if factory == nil {
		return nil, errors.New("agent runtime registry factory is required")
	}
	return &Registry{
		factory: factory,
		entries: make(map[contracts.AgentID]ManagedRuntime),
	}, nil
}

// Activate 把已持久化的 execution 交给所属 Agent 的 runtime。
func (r *Registry) Activate(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	lifecycle runtimecontract.ExecutionLifecycle,
) error {
	if ctx == nil {
		return errors.New("runtime activation context is required")
	}
	if lifecycle == nil {
		return errors.New("runtime execution lifecycle is required")
	}
	if err := execution.Validate(); err != nil {
		return err
	}
	runtime, err := r.getOrCreate(ctx, execution.AgentID)
	if err != nil {
		return err
	}
	if err := runtime.Activate(ctx, execution, lifecycle); err != nil {
		return fmt.Errorf("activate agent runtime: %w", err)
	}
	return nil
}

// Cancel 在 runtime 存在时转发已持久化的控制请求。
func (r *Registry) Cancel(
	ctx context.Context,
	agentID contracts.AgentID,
	executionID contracts.AgentExecutionID,
	outcome executionmodel.ExecutionOutcome,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("runtime cancellation context is required")
	}
	r.mu.Lock()
	entry, found := r.entries[agentID]
	r.mu.Unlock()
	if !found {
		return false, nil
	}
	cancelled, err := entry.Cancel(ctx, executionID, outcome)
	if err != nil {
		return cancelled, fmt.Errorf("cancel agent runtime: %w", err)
	}
	return cancelled, nil
}

// Close 停止所有已创建的 runtime，并拒绝后续激活请求。
func (r *Registry) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("runtime registry close context is required")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	entries := make([]ManagedRuntime, 0, len(r.entries))
	for agentID, runtime := range r.entries {
		entries = append(entries, runtime)
		delete(r.entries, agentID)
	}
	r.mu.Unlock()
	var firstErr error
	for _, runtime := range entries {
		if err := runtime.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Registry) getOrCreate(
	ctx context.Context,
	agentID contracts.AgentID,
) (ManagedRuntime, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("agent runtime registry is closed")
	}
	if entry, found := r.entries[agentID]; found {
		r.mu.Unlock()
		return entry, nil
	}
	r.mu.Unlock()

	candidate, err := r.factory.New(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("create agent runtime: %w", err)
	}
	if candidate == nil {
		return nil, errors.New("agent runtime factory returned nil runtime")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = candidate.Close(context.WithoutCancel(ctx))
		return nil, errors.New("agent runtime registry is closed")
	}
	if entry, found := r.entries[agentID]; found {
		r.mu.Unlock()
		_ = candidate.Close(context.WithoutCancel(ctx))
		return entry, nil
	}
	r.entries[agentID] = candidate
	r.mu.Unlock()
	return candidate, nil
}
