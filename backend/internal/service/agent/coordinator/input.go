package coordinator

import (
	"context"
	"fmt"
	"praxis/internal/identity"
	executionservice "praxis/internal/service/agent/execution"
	queueservice "praxis/internal/service/task"
)

type StartResult struct {
	executionservice.SendInputResult
	ActivationError string
}

func (s *Coordinator) SendInput(ctx context.Context, input executionservice.SendInput) (StartResult, error) {
	if err := s.enter(ctx); err != nil {
		return StartResult{}, err
	}
	defer s.running.Done()
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	result, err := s.operations.SendInput(ctx, input)
	if err != nil {
		return StartResult{}, err
	}
	response := StartResult{SendInputResult: result}

	ctx = context.WithoutCancel(ctx)
	if err := s.Activate(ctx, result.Execution); err != nil {
		response.ActivationError = err.Error()

	}
	return response, nil
}

func (s *Coordinator) EnqueueTask(ctx context.Context, input queueservice.EnqueueInput) (queueservice.EnqueueResult, error) {
	if err := s.enter(ctx); err != nil {
		return queueservice.EnqueueResult{}, err
	}
	defer s.running.Done()
	return s.queue.EnqueueTask(ctx, input)
}

// Restore 必须在外部准入开放前调用，先协调上次进程的未知副作用，再恢复任务与队列。
func (s *Coordinator) Restore(ctx context.Context) error {
	if err := s.operations.ReconcileToolResults(ctx); err != nil {
		return fmt.Errorf("reconcile tool results: %w", err)
	}
	if err := s.restore(ctx, s.recovery); err != nil {
		return fmt.Errorf("restore execution: %w", err)
	}
	return nil
}

func (s *Coordinator) Pause(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.activeTasks.Pause(agentID, executionID)
}

func (s *Coordinator) Stop(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.activeTasks.Stop(agentID, executionID)
}
