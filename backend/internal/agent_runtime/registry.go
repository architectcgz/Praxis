package agentruntime

import (
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"

	"context"
	"errors"
	"fmt"
	"sync"
)

// ManagedRuntime 只暴露进程内生命周期操作。
// 持久化产品状态仍由 task application service 负责。
type ManagedRuntime interface {
	Activate(context.Context, taskmodel.Task) error
	StartNext(context.Context) (bool, error)
	Close(context.Context) error
}

// ManagedRuntimeFactory 在首次使用时为 Agent 创建 runtime。
type ManagedRuntimeFactory interface {
	New(context.Context, contracts.AgentID) (ManagedRuntime, error)
}

// Registry 管理每个 Agent 的进程内 runtime。
// 产品所有权仍以 日志投影的活动 Task 约束为准。
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

// Activate 把已持久化的 task 交给所属 Agent 的 runtime。
func (r *Registry) Activate(
	ctx context.Context,
	task taskmodel.Task,
) error {
	if ctx == nil {
		return errors.New("runtime activation context is required")
	}
	if task.Status != taskmodel.TaskStarting {
		return nil
	}
	runtime, err := r.getOrCreate(ctx, task.AgentID)
	if err != nil {
		return err
	}
	if err := runtime.Activate(ctx, task); err != nil {
		return fmt.Errorf("activate agent runtime: %w", err)
	}
	return nil
}

// StartNext 唤醒指定 Agent 的队列；首次入队时创建 runtime，忙碌时由当前回合结束后继续领取。
func (r *Registry) StartNext(ctx context.Context, agentID contracts.AgentID) (bool, error) {
	if ctx == nil {
		return false, errors.New("runtime queue context is required")
	}
	runtime, err := r.getOrCreate(ctx, agentID)
	if err != nil {
		return false, err
	}
	return runtime.StartNext(ctx)
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
