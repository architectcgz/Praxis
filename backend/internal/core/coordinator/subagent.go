package coordinator

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/loop"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

func (s *Coordinator) startSubagent(ctx context.Context, parent *agentrun.Run, parentActive *activeRun, assignment agentrun.Assignment, callID string) (agentrun.SubagentStatus, error) {
	parentRecord := parent.Snapshot()
	if !parent.Main() {
		return agentrun.SubagentStatus{}, fmt.Errorf("subagents cannot delegate")
	}
	catalog, err := tool.Restrict(parentActive.config.Tools, assignment.AllowedTools)
	if err != nil {
		return agentrun.SubagentStatus{}, err
	}
	s.mu.Lock()
	if s.closed || parentActive.ctx.Err() != nil {
		s.mu.Unlock()
		return agentrun.SubagentStatus{}, fmt.Errorf("parent execution is closing")
	}
	count := 0
	for key, active := range s.runs {
		if active.sessionID == parentRecord.SessionID && key.agentID != identity.AgentID(parentRecord.SessionID) {
			count++
		}
	}
	if count >= 10 {
		s.mu.Unlock()
		return agentrun.SubagentStatus{}, fmt.Errorf("session subagent limit of 10 is reached")
	}
	agentID := identity.AgentID(s.runDependencies.IDs.New("agent"))
	active := newActiveRun(ctx, parentRecord.SessionID)
	config := *parentActive.config
	config.Tools = catalog
	active.config = &config
	active.parent = parent
	key := runKey{agentID: agentID}
	s.runs[key] = active
	s.mu.Unlock()
	childCtx, cancelTimeout := context.WithTimeout(parentActive.ctx, time.Duration(assignment.TimeoutSeconds)*time.Second)
	stopParent := context.AfterFunc(childCtx, func() { active.cancel(context.Cause(childCtx)) })
	definition, err := config.Definition(agentmodel.DefinitionDelegate)
	if err != nil {
		s.releaseFailedChild(key, active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	input := taskmodel.Task{ID: identity.TaskID(s.runDependencies.IDs.New("task")), SessionID: parentRecord.SessionID, Prompt: assignment.Task}
	child, err := agentrun.PrepareSubagent(active.ctx, s.runDependencies, agentrun.SubagentInput{
		Task:         input,
		AgentID:      agentID,
		ParentRunID:  parentRecord.ID,
		ParentCallID: callID,
		Definition:   definition,
		Model:        parentRecord.Config.Model,
		Owner:        parent.Session,
	})
	if err != nil {
		if child != nil {
			_, saveErr := child.Settle(context.WithoutCancel(ctx), sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, "")
			err = errors.Join(err, saveErr)
		}
		s.releaseFailedChild(key, active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	child.BindParent(parent)
	parent.RegisterSubagent(agentrun.SubagentHandle{
		AgentID: agentID,
		Run:     child,
		Cancel:  active.cancel,
		Done:    active.done,
	})
	active.onDone = func() error {
		defer stopParent()
		defer cancelTimeout()
		if !child.TerminalSaved() {
			err := loop.NewFailure(loop.FailureStorage, fmt.Errorf("subagent %s did not save its terminal result", agentID))
			parent.Cancel(err)
			return err
		}
		return parent.AcceptSubagentResult(context.WithoutCancel(parentActive.ctx), child.ResultForParent())
	}
	s.loop.Emitter.Emit(coreevent.Event{Source: child.EventSource(""), Payload: coreevent.SubagentStarted{
		Name:          definition.Name,
		DefinitionID:  definition.ID,
		CreatedAt:     child.Snapshot().CreatedAt,
		Run:           child.Snapshot().Snapshot(),
		Entries:       child.Entries(),
		ParentAgentID: parentRecord.AgentID,
		ParentRunID:   parentRecord.ID,
		CallID:        callID,
	}})
	s.mu.Lock()
	err = s.launchLocked(child, child.Snapshot().ID, active)
	s.mu.Unlock()
	if err != nil {
		_, saveErr := child.Settle(context.WithoutCancel(ctx), sessionentry.OutcomeInterrupted, sessionentry.RunFailureCancelled, "")
		err = errors.Join(err, saveErr)
		s.releaseFailedChild(key, active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	return agentrun.SubagentStatus{AgentID: agentID, Run: child.Snapshot().Snapshot()}, nil
}
func (s *Coordinator) releaseFailedChild(key runKey, active *activeRun) {
	s.mu.Lock()
	delete(s.runs, key)
	close(active.ready)
	close(active.done)
	s.mu.Unlock()
	active.cancel(nil)
}
