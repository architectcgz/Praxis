package coordinator

import (
	"context"
	"errors"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/loop"
	taskmodel "praxis/internal/core/task"
)

func (r *Coordinator) run(ctx context.Context, run *agentrun.Run, active *activeRun) {
	defer func() {
		close(active.done)
		if active.parent != nil {
			active.parent.WakeSubagentWaiters()
		}
	}()
	record := run.Snapshot()
	runner := *r.loop
	if runner.Provider == nil {
		runner.Provider = active.config.Provider
	}
	if runner.Tools == nil {
		runner.Tools = active.config.Tools
	}
	err := run.Execute(ctx, &runner, func(prepareCtx context.Context) (loop.ToolExecutor, error) {
		info, err := r.sessionRecords.Get(prepareCtx, record.SessionID)
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
		base := r.tools.ForRun(run, project.Path, active.config.Tools, active.config.ApprovalMode, runner.Provider, run, func(waitCtx context.Context) error { return r.waitForToolApproval(waitCtx, active) })
		return &nativeExecutor{
			base:    base,
			catalog: catalog,
			run:     run,
			emitter: r.loop.Emitter,
		}, nil
	})
	if err != nil {
		r.logger.Errorf("run failed run_id=%s: %v", record.ID, err)
	}
	if !run.Main() {
		if active.onDone != nil {
			if callbackErr := active.onDone(); callbackErr != nil {
				r.logger.Errorf("deliver subagent result: %v", callbackErr)
				active.parent.Cancel(loop.NewFailure(loop.FailureStorage, callbackErr))
			}
		}
		active.cancel(nil)
		active.parent.CompleteSubagent(record.AgentID, run.Snapshot().Snapshot())
		r.mu.Lock()
		delete(r.runs, runKey{record.AgentID})
		r.mu.Unlock()
		r.loop.Emitter.Emit(coreevent.Event{Source: run.EventSource(""), Payload: coreevent.SubagentReleased{}})
		return
	}
	queueCtx, cancel := r.transitionContext()
	defer cancel()
	unlock, _ := r.lockSession(context.Background(), record.SessionID)
	r.mu.Lock()
	delete(r.runs, runKey{record.AgentID})
	active.run = nil
	r.mu.Unlock()
	var next *activeRun
	var binding *sessionSchedule
	var input taskmodel.Task
	var scheduleErr error
	if err == nil && run.Snapshot().Outcome == sessionentry.OutcomeCompleted && !errors.Is(context.Cause(active.ctx), agentrun.ErrInterrupted) {
		input, binding, next, scheduleErr = r.claimTaskLocked(queueCtx, record.SessionID)
	}
	unlock()
	active.cancel(nil)
	if next != nil {
		scheduleErr = r.startNext(queueCtx, input, runKey{record.AgentID}, binding, next)
	}
	if scheduleErr != nil {
		r.logger.Errorf("prepare queued task session=%s: %v", record.SessionID, scheduleErr)
	}
}
