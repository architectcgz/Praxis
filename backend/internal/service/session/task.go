package session

import (
	"context"
	"errors"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"strings"
)

// TaskInput 仅接收 Session 与已规范化正文，不要求调用方分配任何执行身份。
type TaskInput struct {
	SessionID identity.SessionID
	Prompt    string
}

// CreateTask 只创建 Session 输入并排队，不查询 Agent、解析模型或创建 Execution。
// 与会话删除共享生命周期锁，避免删除成功后又把输入放回队列。
func (s *Service) CreateTask(ctx context.Context, input TaskInput) (sessionmodel.Task, error) {
	prompt, err := s.hookDispatcher.BeforeUserInput(ctx, hook.UserInput{SessionID: input.SessionID, Prompt: input.Prompt})
	if err != nil {
		return sessionmodel.Task{}, err
	}
	input.Prompt = strings.TrimSpace(prompt)
	if input.Prompt == "" {
		return sessionmodel.Task{}, errors.New("task prompt is required")
	}
	if err := ctx.Err(); err != nil {
		return sessionmodel.Task{}, err
	}
	s.selectionMu.Lock()
	defer s.selectionMu.Unlock()
	if err := RequireWritable(ctx, s.sessions, input.SessionID); err != nil {
		return sessionmodel.Task{}, err
	}
	value := sessionmodel.NewTask(identity.TaskID(s.ids.New("task")), input.SessionID, input.Prompt, s.clock.Now())
	queue := s.taskQueues[input.SessionID]
	if queue == nil {
		queue = &sessionmodel.TaskQueue{}
		s.taskQueues[input.SessionID] = queue
	}
	return queue.Enqueue(value)
}

// CancelTask 只移出尚未领取的输入，不改变当前 Execution 的状态。
func (s *Service) CancelTask(ctx context.Context, sessionID identity.SessionID, id identity.TaskID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	queue, ok := s.TaskQueue(sessionID)
	if !ok {
		return errorcode.ErrNotFound
	}
	return queue.Cancel(id)
}

// TaskQueue 返回已有 Session 队列，不隐式创建或恢复输入。
// 外部只负责领取与交接，队列实例仍由 Session Service 拥有。
func (s *Service) TaskQueue(sessionID identity.SessionID) (*sessionmodel.TaskQueue, bool) {
	s.selectionMu.RLock()
	defer s.selectionMu.RUnlock()
	queue, ok := s.taskQueues[sessionID]
	return queue, ok
}

func (s *Service) ListTasks(sessionID identity.SessionID, limit int) []sessionmodel.Task {
	queue, ok := s.TaskQueue(sessionID)
	if !ok {
		return nil
	}
	return queue.List(limit)
}
