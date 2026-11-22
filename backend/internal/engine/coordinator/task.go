package coordinator

import (
	"context"
	"errors"
	"praxis/internal/engine/hook"
	sessionmodel "praxis/internal/engine/session"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/task"
	"strings"
	"time"
)

type SubmitResult struct {
	Task            task.Task
	SchedulingError string
	titleEvent      *event.SessionTitleChanged
}

func (s *Coordinator) SubmitTask(ctx context.Context, input task.Input) (SubmitResult, error) {
	if err := s.enter(); err != nil {
		return SubmitResult{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	schedule := s.schedules[input.SessionID]
	closed := s.closed
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if closed {
		return SubmitResult{}, errCoordinatorClosed
	}
	if closing {
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
	if result.titleEvent != nil {
		s.emitter.Emit(event.Event{
			Source:  event.EventSource{SessionID: input.SessionID},
			Payload: *result.titleEvent,
		})
	}
	s.emitter.Emit(*notification)
	if err := s.schedule(context.WithoutCancel(ctx), input.SessionID); err != nil {
		result.SchedulingError = err.Error()
	}
	return result, nil
}

func (s *Coordinator) enqueueTask(ctx context.Context, input task.Input, task task.Task) (SubmitResult, *event.Event, error) {
	unlock, err := s.lockSession(ctx, input.SessionID)
	if err != nil {
		return SubmitResult{}, nil, err
	}
	defer unlock()
	s.mu.Lock()
	schedule := s.schedules[input.SessionID]
	closed := s.closed
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if closed {
		return SubmitResult{}, nil, errCoordinatorClosed
	}
	if closing {
		return SubmitResult{}, nil, errorcode.ErrRequestConflict
	}
	if schedule == nil {
		schedule, err = s.loadSchedule(ctx, input.SessionID)
		if err != nil {
			return SubmitResult{}, nil, err
		}
	}
	result := SubmitResult{Task: task}
	if prior, err := schedule.session.Entry(ctx, identity.TaskInputEntryID(task.ID)); err == nil {
		if prior.TextContent() != task.Prompt {
			return SubmitResult{}, nil, errorcode.ErrRequestConflict
		}
		return result, nil, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return SubmitResult{}, nil, err
	}
	for _, prior := range schedule.queue.List(0) {
		if prior.ID == task.ID {
			if prior.Prompt != task.Prompt {
				return SubmitResult{}, nil, errorcode.ErrRequestConflict
			}
			result.Task = prior
			return result, nil, nil
		}
	}
	title := sessionmodel.TitleFromInput(task.Prompt)
	updatedAt := time.Now().UTC()
	if err := s.sessionRecords.UpdateTitle(ctx, task.SessionID, "", title, updatedAt); err != nil {
		if !errors.Is(err, errorcode.ErrRequestConflict) {
			return SubmitResult{}, nil, err
		}
	} else {
		result.titleEvent = &event.SessionTitleChanged{
			Title:     title,
			UpdatedAt: updatedAt,
		}
	}
	task, err = schedule.queue.Enqueue(task)
	if err != nil {
		return SubmitResult{}, nil, err
	}
	result.Task = task
	return result, new(taskQueueEvent(schedule)), nil
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
	schedule := s.schedules[sessionID]
	valid := schedule != nil && !schedule.closing
	s.mu.Unlock()
	if !valid {
		unlock()
		return errorcode.ErrRequestConflict
	}
	err = schedule.queue.Cancel(id)
	notification := taskQueueEvent(schedule)
	unlock()
	if err == nil {
		s.emitter.Emit(notification)
	}
	return err
}

func (s *Coordinator) PauseQueue(ctx context.Context, id identity.SessionID) error {
	return s.changeQueue(ctx, id, true)
}

func (s *Coordinator) ResumeQueue(ctx context.Context, id identity.SessionID) error {
	return s.changeQueue(ctx, id, false)
}

func (s *Coordinator) changeQueue(ctx context.Context, id identity.SessionID, pause bool) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	schedule := s.schedules[id]
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if schedule == nil {
		unlock()
		return errorcode.ErrNotFound
	}
	if closing {
		unlock()
		return errorcode.ErrRequestConflict
	}
	if pause {
		schedule.queue.Pause()
	} else {
		schedule.queue.Resume()
	}
	notification := taskQueueEvent(schedule)
	unlock()
	s.emitter.Emit(notification)
	if !pause {
		s.mu.Lock()
		active := schedule.active
		s.mu.Unlock()
		// 显式恢复可以等待已取消的旧执行收尾，不能让恢复意图丢在占用窗口里。
		if active != nil && active.ctx.Err() != nil {
			select {
			case <-active.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return s.schedule(ctx, id)
	}
	return nil
}

func taskQueueEvent(schedule *sessionSchedule) event.Event {
	return event.Event{
		Source: event.EventSource{
			AgentID:   identity.AgentID(schedule.sessionID),
			SessionID: schedule.sessionID,
		},
		Payload: event.TaskQueueChanged{
			Tasks:  schedule.queue.List(0),
			Paused: schedule.queue.Paused(),
		},
	}
}

func (s *Coordinator) createTask(ctx context.Context, input task.Input) (task.Task, error) {
	prompt, err := s.hookDispatcher.BeforeUserInput(ctx, hook.UserInput{
		SessionID: input.SessionID,
		Prompt:    input.Prompt,
	})
	if err != nil {
		return task.Task{}, err
	}
	if prompt != input.Prompt {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" || len(prompt) > task.MaxInputBytes {
			return task.Task{}, errorcode.ErrInvalidValue
		}
	}
	return task.NewTask(input.SubmissionID, input.SessionID, prompt, time.Now().UTC()), nil
}
