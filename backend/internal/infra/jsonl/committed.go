package jsonl

import (
	"context"
	"encoding/json"
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
)

// ObserveCommitted 在装配时注册事务后通知；观察者失败只写诊断日志。
func (s *Store) ObserveCommitted(dispatcher *hooks.Dispatcher, onError func(string, ...any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = dispatcher
	s.hookError = onError
}
func (s *Store) notifyCommitted(events []Event) {
	if !s.hooks.HasCommittedHooks() {
		return
	}
	for _, event := range events {
		fact := hooks.CommittedEvent{}
		switch event.Collection {
		case "message":
			var record messageRecord
			if json.Unmarshal(event.Payload, &record) != nil {
				continue
			}
			message := record.Data
			if message.Role != sessionmodel.RoleUser && !(message.Role == sessionmodel.RoleAssistant && (message.StopReason == "completed" || message.StopReason == "stop_sequence")) {
				continue
			}
			fact.Identity = hooks.Identity{AgentID: contracts.AgentID(message.AuthorID), TaskID: contracts.TaskID(message.TaskID), TurnID: contracts.TurnID(message.TurnID)}
			fact.Message = &message
		case "task":
			var task taskmodel.Task
			if json.Unmarshal(event.Payload, &task) != nil || task.Status != taskmodel.TaskEnded {
				continue
			}
			fact.Identity = hooks.Identity{SessionID: task.SessionID, AgentID: task.AgentID, TaskID: task.ID}
			fact.Task = &task
		default:
			continue
		}
		if fact.SessionID == "" {
			// 通知在互斥锁外，根据不可变 Task 归属补齐身份。
			task, err := (TaskRepository{s}).Get(context.Background(), fact.TaskID)
			if err != nil {
				continue
			}
			fact.SessionID, fact.AgentID = task.SessionID, task.AgentID
		}
		if err := s.hooks.OnCommitted(context.Background(), fact); err != nil && s.hookError != nil {
			s.hookError("committed hook failed: %v", err)
		}
	}
}
