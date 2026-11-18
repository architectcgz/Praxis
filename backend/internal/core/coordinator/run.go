package coordinator

import (
	"context"
	"errors"
	agentrun "praxis/internal/core/agent/run"
	entryPayload "praxis/internal/core/entry/payload"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/loop"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
)

func (r *Coordinator) run(ctx context.Context, run *agentrun.Run, active *activeRun) {
	defer func() {
		close(active.done)
		if active.parent != nil {
			active.parent.WakeSubagentWaiters()
		}
	}()
	runner := *r.loop
	if runner.Provider == nil {
		runner.Provider = active.config.Provider
	}
	if runner.Tools == nil {
		runner.Tools = active.config.Tools
	}
	err := run.Execute(ctx, &runner, func(prepareCtx context.Context) (loop.ToolExecutor, error) {
		info, err := r.sessionRecords.Get(prepareCtx, run.SessionID())
		if err != nil {
			return nil, loop.NewFailure(loop.FailureStorage, err)
		}
		project, err := r.projects.Get(prepareCtx, info.ProjectID)
		if err != nil {
			return nil, loop.NewFailure(loop.FailureStorage, err)
		}
		catalog, err := r.collaborationTools(run, active, active.config.Tools)
		if err != nil {
			return nil, err
		}
		runner.Tools = catalog
		base := r.tools.ForRun(run, project.Path, active.config.Tools, active.config.ApprovalMode, runner.Provider, run)
		return &nativeExecutor{
			base:    base,
			catalog: catalog,
			run:     run,
			emitter: r.loop.Emitter,
		}, nil
	})
	if err != nil {
		r.logger.Errorf("run failed run_id=%s: %v", run.ID(), err)
	}
	if !run.Main() {
		if loop.FailureClassification(err) == loop.FailureStorage {
			active.parent.Cancel(err)
		}
		if active.onDone != nil {
			if callbackErr := active.onDone(); callbackErr != nil {
				r.logger.Errorf("deliver subagent result: %v", callbackErr)
				active.parent.Cancel(loop.NewFailure(loop.FailureStorage, callbackErr))
			}
		}
		active.cancel(nil)
		active.parent.CompleteSubagent(run.AgentID(), run.Snapshot())
		r.loop.Emitter.Emit(coreevent.Event{Source: run.EventSource(""), Payload: coreevent.SubagentReleased{}})
		return
	}
	queueCtx, cancel := r.transitionContext()
	defer cancel()
	unlock, _ := r.lockSession(context.Background(), run.SessionID())
	r.mu.Lock()
	current := r.schedules[run.SessionID()]
	current.active = nil
	r.mu.Unlock()
	storageErr := err
	if cause := context.Cause(ctx); loop.FailureClassification(cause) == loop.FailureStorage {
		storageErr = cause
	}
	storageFailed := loop.FailureClassification(storageErr) == loop.FailureStorage
	var notification coreevent.Event
	if storageFailed {
		current.queue.Pause()
		notification = taskQueueEvent(current)
	}
	var next *activeRun
	var binding *sessionSchedule
	var input taskmodel.Task
	var scheduleErr error
	if err == nil && run.Outcome() == entryPayload.OutcomeCompleted && !errors.Is(context.Cause(active.ctx), agentrun.ErrInterrupted) {
		input, binding, next, scheduleErr = r.claimTaskLocked(queueCtx, run.SessionID())
	}
	unlock()
	active.cancel(nil)
	if storageFailed {
		r.loop.Emitter.Emit(notification)
		r.emitQueueStorageFailure(run.SessionID(), storageErr)
	}
	if next != nil {
		scheduleErr = r.startNext(queueCtx, input, binding, next)
	}
	if scheduleErr != nil {
		r.logger.Errorf("prepare queued task session=%s: %v", run.SessionID(), scheduleErr)
	}
}

func (r *Coordinator) emitQueueStorageFailure(sessionID identity.SessionID, err error) {
	r.loop.Emitter.Emit(coreevent.Event{
		Source:  coreevent.EventSource{SessionID: sessionID},
		Payload: coreevent.TaskQueueStorageFailed{Message: err.Error()},
	})
}
