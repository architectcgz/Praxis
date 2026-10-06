// Package task 提供 runtime 所需的 Task 创建、开始和结束接口，不负责 Agent 控制或执行调度。
package task

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/service/runtime/task/lifecycle"
	"praxis/internal/service/runtime/task/start"
)

// Service 汇集 Task 准入和生命周期；持久化操作由各服务保证原子性。
type Service struct {
	*start.Service
	lifecycle *lifecycle.Service
}

// NewService 组装回合接口；缺少任一依赖时返回错误。
func NewService(starts *start.Service, lifecycle *lifecycle.Service) (*Service, error) {
	if starts == nil || lifecycle == nil {
		return nil, errors.New("task start and lifecycle services are required")
	}
	return &Service{
		Service:   starts,
		lifecycle: lifecycle,
	}, nil
}

// Start 确认指定回合开始；重复确认保持开始时间不变。
func (s *Service) Start(ctx context.Context, taskID contracts.TaskID) error {
	return s.lifecycle.Start(ctx, taskID)
}

// End 原子结算回合及关联任务，返回经过控制命令仲裁的持久化终态。
func (s *Service) End(ctx context.Context, taskID contracts.TaskID, outcome taskmodel.TaskOutcome, code contracts.TaskFailureCode, message string) (taskmodel.Task, error) {
	return s.lifecycle.End(ctx, taskID, outcome, code, message)
}

// Abort 收敛未被 runtime 接管的 Task：结算为中断并释放进程内执行绑定。
// runtime 激活失败时若不收敛，Agent 会永久停留在执行中且队列无法继续。
func (s *Service) Abort(ctx context.Context, taskID contracts.TaskID) error {
	if ctx == nil {
		return errors.New("task abort context is required")
	}
	ended, err := s.lifecycle.End(ctx, taskID, taskmodel.TaskInterrupted, contracts.TaskFailureInterrupted, "")
	if err != nil {
		return err
	}
	s.ReleaseExecution(ended.AgentID, taskID)
	return nil
}
