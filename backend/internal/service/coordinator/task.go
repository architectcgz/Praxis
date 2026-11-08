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
	result, notification, err := s.enqueueTask(ctx, input, task)
	if err != nil || notification == nil {
		return result, err
	}
	s.loop.Emitter.Emit(*notification)
	if err := s.scheduleBinding(context.WithoutCancel(ctx), input.SessionID, result.BindingID, true); err != nil {
		result.SchedulingError = err.Error()
	}
	return result, nil
}

func (s *Coordinator) enqueueTask(ctx context.Context, input sessionservice.TaskInput, task sessionmodel.Task) (SubmitResult, *coreevent.Event, error) {
	unlock, err := s.lockSession(ctx, input.SessionID)
	if err != nil {
		return SubmitResult{}, nil, err
	}
	defer unlock()
	s.mu.Lock()
	binding := s.bindings[input.SessionID]
	valid := binding == nil || !binding.closing && binding.id == input.BindingID && binding.branchID == input.BranchID
	s.mu.Unlock()
	if !valid {
		return SubmitResult{}, nil, errorcode.ErrRequestConflict
	}
	if binding == nil {
		binding, err = s.bindSubmission(ctx, input)
		if err != nil {
			return SubmitResult{}, nil, err
		}
	}
	result := SubmitResult{
		Task:      task,
		BindingID: binding.id,
	}
	if prior, err := s.sessions.FindRunByRequest(ctx, identity.AgentID(input.SessionID), identity.RequestID(task.ID)); err == nil {
		if prior.BranchID != binding.branchID {
			return SubmitResult{}, nil, errorcode.ErrRequestConflict
		}
		path, err := s.sessions.Path(ctx, input.SessionID, prior.InputEntryID)
		if err != nil {
			return SubmitResult{}, nil, err
		}
		if len(path) == 0 || path[len(path)-1].TextContent() != task.Prompt {
			return SubmitResult{}, nil, errorcode.ErrRequestConflict
		}
		return result, nil, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return SubmitResult{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, prior := range binding.queue.List(0) {
		if prior.ID == task.ID {
			if prior.Prompt != task.Prompt {
				return SubmitResult{}, nil, errorcode.ErrRequestConflict
			}
			result.Task = prior
			return result, nil, nil
		}
	}
	task, err = binding.queue.Enqueue(task)
	if err != nil {
		return SubmitResult{}, nil, err
	}
	result.Task = task
	return result, new(taskQueueEvent(binding)), nil
}

func (s *Coordinator) DeleteSession(ctx context.Context, id identity.SessionID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	s.mu.Lock()
	for _, active := range s.runs {
		if active.sessionID == id {
			s.mu.Unlock()
			return agentmodel.ErrExecuting
		}
	}
	s.mu.Unlock()
	if err := s.sessions.DeleteSession(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	s.releaseSessionStateLocked(id)
	s.mu.Unlock()
	return nil
}

func (s *Coordinator) CancelTask(ctx context.Context, sessionID identity.SessionID, bindingID string, id identity.TaskID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	binding := s.bindings[sessionID]
	if binding == nil || binding.id != bindingID || binding.closing {
		s.mu.Unlock()
		unlock()
		return errorcode.ErrRequestConflict
	}
	err = binding.queue.Cancel(id)
	notification := taskQueueEvent(binding)
	s.mu.Unlock()
	unlock()
	if err == nil {
		s.loop.Emitter.Emit(notification)
	}
	return err
}

func taskQueueEvent(binding *sessionBinding) coreevent.Event {
	return coreevent.Event{
		Source: coreevent.EventSource{
			AgentID:   identity.AgentID(binding.sessionID),
			SessionID: binding.sessionID,
			BranchID:  binding.branchID,
			BindingID: binding.id,
		},
		Payload: coreevent.TaskQueueChanged{Tasks: binding.queue.List(0)},
	}
}
