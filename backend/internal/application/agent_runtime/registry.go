// Package agentruntime owns process-local Agent runtime lifecycle management.
package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	runtimecontract "praxis/internal/runtime"
)

// ManagedRuntime only exposes process-local lifecycle operations. Durable
// product state remains owned by the execution application services.
type ManagedRuntime interface {
	Activate(context.Context, domainexecution.AgentExecution, runtimecontract.ExecutionLifecycle) error
	Cancel(context.Context, domainfoundation.AgentExecutionID, domainexecution.ExecutionOutcome) error
	Close(context.Context) error
}

// ManagedRuntimeFactory constructs a runtime for one Agent on first use.
type ManagedRuntimeFactory interface {
	New(context.Context, domainfoundation.AgentID) (ManagedRuntime, error)
}

// Registry owns the process-local runtime generation for each Agent. SQLite's
// active-execution constraint remains the authority for product ownership.
type Registry struct {
	factory ManagedRuntimeFactory

	mu             sync.Mutex
	entries        map[domainfoundation.AgentID]registryEntry
	nextGeneration uint64
	closed         bool
}

type registryEntry struct {
	generation uint64
	runtime    ManagedRuntime
}

// NewRegistry creates an empty registry backed by the supplied runtime factory.
func NewRegistry(factory ManagedRuntimeFactory) (*Registry, error) {
	if factory == nil {
		return nil, errors.New("agent runtime registry factory is required")
	}
	return &Registry{
		factory: factory,
		entries: make(map[domainfoundation.AgentID]registryEntry),
	}, nil
}

// Activate delivers a durable execution to its Agent-local runtime.
func (r *Registry) Activate(
	ctx context.Context,
	execution domainexecution.AgentExecution,
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

// Cancel forwards a durable control request to the current runtime, if any.
func (r *Registry) Cancel(
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

// Close stops each materialized runtime and rejects later activation requests.
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
	entries := make([]registryEntry, 0, len(r.entries))
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

func (r *Registry) getOrCreate(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (ManagedRuntime, error) {
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
	r.entries[agentID] = registryEntry{generation: r.nextGeneration, runtime: candidate}
	r.mu.Unlock()
	return candidate, nil
}
