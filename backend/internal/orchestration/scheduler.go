// Package orchestration coordinates durable application use cases.
package orchestration

import (
	executionmodel "praxis/internal/execution"

	"context"
	"errors"
	"fmt"

	runtimecontract "praxis/internal/runtime"
)

// RuntimeActivator activates a durable execution in its process-local runtime.
type RuntimeActivator interface {
	Activate(context.Context, executionmodel.AgentExecution, runtimecontract.ExecutionLifecycle) error
}

// SchedulerConfig describes the durable execution source and runtime boundary.
type SchedulerConfig struct {
	Runtime RuntimeActivator
}

// Scheduler discovers durable starting executions and requests activation. It
// cannot create executions or carry user input payloads.
type Scheduler struct {
	runtime RuntimeActivator
}

// NewScheduler validates and creates an execution scheduler.
func NewScheduler(config SchedulerConfig) (*Scheduler, error) {
	if config.Runtime == nil {
		return nil, errors.New("execution scheduler runtime is required")
	}
	return &Scheduler{runtime: config.Runtime}, nil
}

// TryActivate 将 service 已提交的 Execution 交给当前进程内的 runtime。
func (s *Scheduler) TryActivate(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	lifecycle runtimecontract.ExecutionLifecycle,
) error {
	if ctx == nil {
		return errors.New("execution activation context is required")
	}
	if lifecycle == nil {
		return errors.New("execution lifecycle is required")
	}
	if execution.Status != executionmodel.ExecutionStarting {
		return nil
	}
	if err := s.runtime.Activate(ctx, execution, lifecycle); err != nil {
		return fmt.Errorf("activate execution %s: %w", execution.ID, err)
	}
	return nil
}
