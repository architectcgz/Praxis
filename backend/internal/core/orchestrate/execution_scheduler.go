package orchestrate

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

// RuntimeActivator is a generation-fenced registry boundary. It may lose a
// notification because starting executions remain discoverable in SQLite.
type RuntimeActivator interface {
	Activate(ctx context.Context, execution domain.AgentExecution) error
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

func (s *ExecutionScheduler) TryActivate(ctx context.Context, agentID domain.AgentID) error {
	if ctx == nil {
		return errors.New("execution activation context is required")
	}
	execution, err := s.executions.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if execution.Status != domain.ExecutionStarting {
		return nil
	}
	if err := s.runtime.Activate(ctx, execution); err != nil {
		return fmt.Errorf("activate execution %s: %w", execution.ID, err)
	}
	return nil
}

// ActivateStarting is used after recovery because runtime notifications are
// an optimization, not a source of runnable execution state.
func (s *ExecutionScheduler) ActivateStarting(ctx context.Context, limit int) error {
	if ctx == nil {
		return errors.New("starting execution scan context is required")
	}
	executions, err := s.executions.ListStarting(ctx, limit)
	if err != nil {
		return err
	}
	for _, execution := range executions {
		if err := s.runtime.Activate(ctx, execution); err != nil {
			return fmt.Errorf("activate execution %s: %w", execution.ID, err)
		}
	}
	return nil
}

var _ ExecutionActivation = (*ExecutionScheduler)(nil)
