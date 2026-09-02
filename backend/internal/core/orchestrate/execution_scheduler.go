package orchestrate

import (
	"context"
	"errors"
	"fmt"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"

	"praxis/internal/core/persistence"
	coreruntime "praxis/internal/core/runtime"
)

// RuntimeActivator is a generation-fenced registry boundary. It may lose a
// notification because starting executions remain discoverable in SQLite.
type RuntimeActivator interface {
	Activate(context.Context, domainexecution.AgentExecution, coreruntime.ExecutionLifecycle) error
}

type ExecutionSchedulerConfig struct {
	Executions persistence.AgentExecutionRepository
	Runtime    RuntimeActivator
}

// ExecutionScheduler only discovers durable starting executions and requests
// activation. It cannot create executions or carry user input payloads.
type ExecutionScheduler struct {
	executions persistence.AgentExecutionRepository
	runtime    RuntimeActivator
}

func NewExecutionScheduler(config ExecutionSchedulerConfig) (*ExecutionScheduler, error) {
	if config.Executions == nil {
		return nil, errors.New("execution scheduler executions are required")
	}
	if config.Runtime == nil {
		return nil, errors.New("execution scheduler runtime is required")
	}
	return &ExecutionScheduler{executions: config.Executions, runtime: config.Runtime}, nil
}

func (s *ExecutionScheduler) TryActivate(
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

// ActivateStarting activates the complete cursor-scanned set supplied by
// recovery. Runtime notifications remain an optimization.
func (s *ExecutionScheduler) ActivateStarting(
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

var _ ExecutionActivation = (*ExecutionScheduler)(nil)
