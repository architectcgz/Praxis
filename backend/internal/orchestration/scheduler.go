// Package orchestration coordinates durable application use cases.
package orchestration

import (
	"context"
	"errors"
	"fmt"

	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
)

// RuntimeActivator activates a durable execution in its process-local runtime.
type RuntimeActivator interface {
	Activate(context.Context, domainexecution.AgentExecution, coreruntime.ExecutionLifecycle) error
}

// SchedulerConfig describes the durable execution source and runtime boundary.
type SchedulerConfig struct {
	Executions persistence.AgentExecutionRepository
	Runtime    RuntimeActivator
}

// Scheduler discovers durable starting executions and requests activation. It
// cannot create executions or carry user input payloads.
type Scheduler struct {
	executions persistence.AgentExecutionRepository
	runtime    RuntimeActivator
}

// NewScheduler validates and creates an execution scheduler.
func NewScheduler(config SchedulerConfig) (*Scheduler, error) {
	if config.Executions == nil {
		return nil, errors.New("execution scheduler executions are required")
	}
	if config.Runtime == nil {
		return nil, errors.New("execution scheduler runtime is required")
	}
	return &Scheduler{executions: config.Executions, runtime: config.Runtime}, nil
}

// TryActivate looks up one Agent's durable starting execution before notifying
// its runtime. A lost notification is recoverable from persistent state.
func (s *Scheduler) TryActivate(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	lifecycle coreruntime.ExecutionLifecycle,
) error {
	if ctx == nil {
		return errors.New("execution activation context is required")
	}
	if lifecycle == nil {
		return errors.New("execution lifecycle is required")
	}
	execution, err := s.executions.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, domainfoundation.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if execution.Status != domainexecution.ExecutionStarting {
		return nil
	}
	if err := s.runtime.Activate(ctx, execution, lifecycle); err != nil {
		return fmt.Errorf("activate execution %s: %w", execution.ID, err)
	}
	return nil
}

// ActivateStarting activates the cursor-scanned recovery set. Runtime
// notifications remain an optimization rather than the source of truth.
func (s *Scheduler) ActivateStarting(
	ctx context.Context,
	executions []domainexecution.AgentExecution,
	lifecycle coreruntime.ExecutionLifecycle,
) error {
	if ctx == nil {
		return errors.New("starting execution scan context is required")
	}
	if lifecycle == nil {
		return errors.New("execution lifecycle is required")
	}
	for _, execution := range executions {
		if execution.Status != domainexecution.ExecutionStarting {
			return fmt.Errorf("execution %s is not starting", execution.ID)
		}
		if err := s.runtime.Activate(ctx, execution, lifecycle); err != nil {
			return fmt.Errorf("activate execution %s: %w", execution.ID, err)
		}
	}
	return nil
}
