package coordinator

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

type SubmitResult struct {
	Task            sessionmodel.Task
	SchedulingError string
}

// SubmitTask 先创建 Session 待办，再尝试唤醒调度；接受输入不依赖 Agent 是否空闲。
// 入队后调度失败不撤销输入，由返回值说明，避免调用方误以为入队失败而重复提交。
func (s *Coordinator) SubmitTask(ctx context.Context, input sessionservice.TaskInput) (SubmitResult, error) {
	if err := s.enter(ctx); err != nil {
		return SubmitResult{}, err
	}
	defer s.running.Done()
	task, err := s.sessionTasks.CreateTask(ctx, input)
	if err != nil {
		return SubmitResult{}, err
	}
	result := SubmitResult{Task: task}
	if _, err := s.StartNext(context.WithoutCancel(ctx), task.SessionID); err != nil {
		result.SchedulingError = err.Error()
		s.logger.Errorf("schedule session input session_id=%s task_id=%s error=%v", task.SessionID, task.ID, err)
	}
	return result, nil
}

func (s *Coordinator) CancelTask(ctx context.Context, sessionID identity.SessionID, id identity.TaskID) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.running.Done()
	return s.sessionTasks.CancelTask(ctx, sessionID, id)
}
