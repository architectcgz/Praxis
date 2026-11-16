package coordinator

import (
	"context"
	"errors"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"strings"
)

type SubmitResult struct {
	Task taskmodel.Task

	SchedulingError string
}

func (s *Coordinator) SubmitTask(ctx context.Context, input taskmodel.Input) (SubmitResult, error) {
	if err := s.enter(); err != nil {
		return SubmitResult{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	schedule := s.schedules[input.SessionID]
	blocked := schedule != nil && (schedule.reloading || schedule.closing)
	s.mu.Unlock()
	if blocked {
		return SubmitResult{}, errorcode.ErrRequestConflict
	}
	task, err := s.createTask(ctx, input)
	if err != nil {
		return SubmitResult{}, err
	}
	result, notification, err := s.enqueueTask(ctx, input, task)
	if err != nil || notification == nil {
		return result, err
	}
	s.loop.Emitter.Emit(*notification)
	if err := s.schedule(context.WithoutCancel(ctx), input.SessionID, true); err != nil {
		result.SchedulingError = err.Error()
	}
	return result, nil
}

func (s *Coordinator) enqueueTask(ctx context.Context, input taskmodel.Input, task taskmodel.Task) (SubmitResult, *coreevent.Event, error) {
	unlock, err := s.lockSession(ctx, input.SessionID)
	if err != nil {
		return SubmitResult{}, nil, err
	}
	defer unlock()
	s.mu.Lock()
	binding := s.schedules[input.SessionID]
	valid := binding == nil || !binding.closing && !binding.reloading
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
		Task: task,
	}
	if prior, err := binding.session.FindRunByTask(ctx, identity.AgentID(input.SessionID), task.ID); err == nil {
		entry, err := binding.session.Entry(ctx, prior.InputEntryID)
		if err != nil {
			return SubmitResult{}, nil, err
		}
		if entry.TextContent() != task.Prompt {
			return SubmitResult{}, nil, errorcode.ErrRequestConflict
		}
		return result, nil, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return SubmitResult{}, nil, err
	}
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

func (s *Coordinator) CancelTask(ctx context.Context, sessionID identity.SessionID, id identity.TaskID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	binding := s.schedules[sessionID]
	valid := binding != nil && !binding.closing
	s.mu.Unlock()
	if !valid {
		unlock()
		return errorcode.ErrRequestConflict
	}
	err = binding.queue.Cancel(id)
	notification := taskQueueEvent(binding)
	unlock()
	if err == nil {
		s.loop.Emitter.Emit(notification)
	}
	return err
}

func taskQueueEvent(binding *sessionSchedule) coreevent.Event {
	return coreevent.Event{
		Source: coreevent.EventSource{
			AgentID:   identity.AgentID(binding.sessionID),
			SessionID: binding.sessionID,
		},
		Payload: coreevent.TaskQueueChanged{Tasks: binding.queue.List(0), Paused: binding.queue.Paused()},
	}
}

func (s *Coordinator) createTask(ctx context.Context, input taskmodel.Input) (taskmodel.Task, error) {
	prompt, err := s.hookDispatcher.BeforeUserInput(ctx, hook.UserInput{SessionID: input.SessionID, Prompt: input.Prompt})
	if err != nil {
		return taskmodel.Task{}, err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > taskmodel.MaxInputBytes {
		return taskmodel.Task{}, errorcode.ErrInvalidValue
	}
	info, err := s.sessionRecords.Get(ctx, input.SessionID)
	if err != nil {
		return taskmodel.Task{}, err
	}
	if info.State != sessionmodel.SessionActive {
		return taskmodel.Task{}, errorcode.ErrRequestConflict
	}
	return taskmodel.NewTask(input.SubmissionID, input.SessionID, prompt, s.runDependencies.Clock.Now()), nil
}
