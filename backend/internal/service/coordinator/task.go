package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
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
	if err := s.enter(); err != nil {
		return SubmitResult{}, err
	}
	defer s.running.Done()
	task, err := s.sessions.CreateTask(ctx, input)
	if err != nil {
		return SubmitResult{}, err
	}
	result := SubmitResult{Task: task}
	if err := s.scheduleSession(context.WithoutCancel(ctx), task.SessionID, true); err != nil {
		result.SchedulingError = err.Error()
		s.logger.Errorf("schedule session input session_id=%s task_id=%s error=%v", task.SessionID, task.ID, err)
	}
	return result, nil
}

// DeleteSession 在执行生命周期锁内确认会话空闲，再交由 Session 删除元数据和上下文。
func (s *Coordinator) DeleteSession(ctx context.Context, sessionID identity.SessionID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, active := range s.executions {
		if active.sessionID == sessionID {
			return agentmodel.ErrExecuting
		}
	}
	return s.sessions.DeleteSession(ctx, sessionID)
}

func (s *Coordinator) CancelTask(ctx context.Context, sessionID identity.SessionID, id identity.TaskID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	return s.sessions.CancelTask(ctx, sessionID, id)
}
