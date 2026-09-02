package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"

	coreruntime "praxis/internal/core/runtime"
)

// ManagedAgentRuntime is intentionally limited to process-local lifecycle
// signals. It cannot create executions or persist product-owned payloads.
type ManagedAgentRuntime interface {
	Activate(context.Context, domainexecution.AgentExecution, coreruntime.ExecutionLifecycle) error
	Cancel(context.Context, domainfoundation.AgentExecutionID, domainexecution.ExecutionOutcome) error
	Close(context.Context) error
}

type ManagedAgentRuntimeFactory interface {
	New(context.Context, domainfoundation.AgentID) (ManagedAgentRuntime, error)
}

// AgentRuntimeRegistry owns the process-local runtime generation for each
// Agent. SQLite's one-active-execution constraint fences business ownership;
// generation only prevents stale in-memory actors from receiving new signals.
type AgentRuntimeRegistry struct {
	factory ManagedAgentRuntimeFactory

	mu             sync.Mutex
	entries        map[domainfoundation.AgentID]runtimeRegistryEntry
	nextGeneration uint64
	closed         bool
}

type runtimeRegistryEntry struct {
	generation uint64
	runtime    ManagedAgentRuntime
}

func NewAgentRuntimeRegistry(factory ManagedAgentRuntimeFactory) (*AgentRuntimeRegistry, error) {
	if factory == nil {
		return nil, errors.New("agent runtime registry factory is required")
	}
	return &AgentRuntimeRegistry{
		factory: factory,
		entries: make(map[domainfoundation.AgentID]runtimeRegistryEntry),
	}, nil
}

func (r *AgentRuntimeRegistry) Activate(
	ctx context.Context,
	execution domainexecution.AgentExecution,
	lifecycle coreruntime.ExecutionLifecycle,
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

func (r *AgentRuntimeRegistry) Cancel(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	executionID domainfoundation.AgentExecutionID,
	outcome domainexecution.ExecutionOutcome,
) error {
	if ctx == nil {
		return errors.New("runtime cancellation context is required")
	}
	r.mu.Lock()
	entry, found := r.entries[agentID]
	r.mu.Unlock()
	if !found {
		return nil
	}
	if err := entry.runtime.Cancel(ctx, executionID, outcome); err != nil {
		return fmt.Errorf("cancel agent runtime: %w", err)
	}
	return nil
}

func (r *AgentRuntimeRegistry) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("runtime registry close context is required")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	entries := make([]runtimeRegistryEntry, 0, len(r.entries))
	for agentID, entry := range r.entries {
		entries = append(entries, entry)
		delete(r.entries, agentID)
	}
	r.mu.Unlock()
	var firstErr error
	for _, entry := range entries {
		if err := entry.runtime.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *AgentRuntimeRegistry) getOrCreate(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (ManagedAgentRuntime, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("agent runtime registry is closed")
	}
	if entry, found := r.entries[agentID]; found {
		r.mu.Unlock()
		return entry.runtime, nil
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
		return entry.runtime, nil
	}
	r.nextGeneration++
	r.entries[agentID] = runtimeRegistryEntry{generation: r.nextGeneration, runtime: candidate}
	r.mu.Unlock()
	return candidate, nil
}

var (
	_ RuntimeActivator      = (*AgentRuntimeRegistry)(nil)
	_ ExecutionCancellation = (*AgentRuntimeRegistry)(nil)
)
