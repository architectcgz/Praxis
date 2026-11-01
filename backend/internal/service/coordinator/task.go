package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

type SubmitResult struct {
	Task            sessionmodel.Task
	BindingID       string
	BranchID        identity.BranchID
	SchedulingError string
}

func (s *Coordinator) SubmitTask(ctx context.Context, input sessionservice.TaskInput) (SubmitResult, error) {
	if err := s.enter(); err != nil {
		return SubmitResult{}, err
	}
	defer s.running.Done()
	task, err := s.sessions.CreateTask(ctx, input)
	if err != nil {
		return SubmitResult{}, err
	}
	s.mu.Lock()
	binding := s.bindings[input.BindingID]
	created := binding == nil
	if created {
		binding, err = s.bindSubmission(ctx, input)
		if err != nil {
			s.mu.Unlock()
			return SubmitResult{}, err
		}
	} else if binding.closing || binding.sessionID != input.SessionID {
		s.mu.Unlock()
		return SubmitResult{}, errorcode.ErrRequestConflict
	}
	result := SubmitResult{
		Task:      task,
		BindingID: binding.id,
		BranchID:  binding.branchID,
	}
	// 自动分叉后的重试仍可携带来源分支，先确认已提交身份，再校验新输入的分支。
	if prior, err := s.sessions.FindExecutionByRequest(ctx, identity.AgentID(input.SessionID), identity.RequestID(task.ID)); err == nil {
		if prior.BranchID != binding.branchID {
			s.mu.Unlock()
			return SubmitResult{}, errorcode.ErrRequestConflict
		}
		path, err := s.sessions.Path(ctx, input.SessionID, prior.InputEntryID)
		s.mu.Unlock()
		if err != nil {
			return SubmitResult{}, err
		}
		if len(path) == 0 || path[len(path)-1].TextContent() != task.Prompt {
			return SubmitResult{}, errorcode.ErrRequestConflict
		}
		return result, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		s.mu.Unlock()
		return SubmitResult{}, err
	}
	for _, prior := range binding.queue.List(0) {
		if prior.ID == task.ID {
			s.mu.Unlock()
			if prior.Prompt != task.Prompt {
				return SubmitResult{}, errorcode.ErrRequestConflict
			}
			result.Task = prior
			return result, nil
		}
	}
	if !created && binding.branchID != input.BranchID {
		s.mu.Unlock()
		return SubmitResult{}, errorcode.ErrRequestConflict
	}
	task.BranchID = binding.branchID
	task, err = binding.queue.Enqueue(task)
	notification := taskQueueEvent(binding)
	s.mu.Unlock()
	if err != nil {
		return SubmitResult{}, err
	}
	s.loop.Emitter.Emit(notification)
	result.Task = task
	if err := s.scheduleBinding(context.WithoutCancel(ctx), binding.id, true); err != nil {
		result.SchedulingError = err.Error()
	}
	return result, nil
}
func (s *Coordinator) DeleteSession(ctx context.Context, id identity.SessionID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, active := range s.executions {
		if active.sessionID == id {
			return agentmodel.ErrExecuting
		}
	}
	if err := s.sessions.DeleteSession(ctx, id); err != nil {
		return err
	}
	for key, binding := range s.bindings {
		if binding.sessionID == id {
			binding.queue.Clear()
			delete(s.bindings, key)
			delete(s.branchOwners, branchKey{id, binding.branchID})
		}
	}
	return nil
}
func (s *Coordinator) CancelTask(ctx context.Context, bindingID string, id identity.TaskID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	s.mu.Lock()
	binding := s.bindings[bindingID]
	if binding == nil || binding.closing {
		s.mu.Unlock()
		return errorcode.ErrRequestConflict
	}
	err := binding.queue.Cancel(id)
	notification := taskQueueEvent(binding)
	s.mu.Unlock()
	if err == nil {
		s.loop.Emitter.Emit(notification)
	}
	return err
}

func taskQueueEvent(binding *sessionBinding) coreevent.Event {
	return coreevent.Event{
		Scope: coreevent.Scope{
			SessionID: binding.sessionID,
			BranchID:  binding.branchID,
			BindingID: binding.id,
			AgentID:   identity.AgentID(binding.sessionID),
		},
		Payload: coreevent.TaskQueueChanged{Tasks: binding.queue.List(0)},
	}
}
