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
	catalog, err := tool.Restrict(parentActive.config.Tools, assignment.AllowedTools)
	if err != nil {
		return agentrun.SubagentStatus{}, err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || parentActive.ctx.Err() != nil {
		return agentrun.SubagentStatus{}, fmt.Errorf("parent execution is closing")
	}
	if parent.ActiveSubagents() >= 10 {
		return agentrun.SubagentStatus{}, fmt.Errorf("session subagent limit of 10 is reached")
	}
	agentID := identity.AgentID(s.runDependencies.IDs.New("agent"))
	active := newActiveRun(ctx, parent.SessionID())
	config := *parentActive.config
	config.Tools = catalog
	active.config = &config
	active.parent = parent
	childCtx, cancelTimeout := context.WithTimeout(parentActive.ctx, time.Duration(assignment.TimeoutSeconds)*time.Second)
	stopParent := context.AfterFunc(childCtx, func() { active.cancel(context.Cause(childCtx)) })
	definition, err := config.Definition(agentmodel.DefinitionDelegate)
	if err != nil {
		s.releaseFailedChild(active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	input := taskmodel.Task{
		ID:        identity.TaskID(s.runDependencies.IDs.New("task")),
		SessionID: parent.SessionID(),
		Prompt:    assignment.Task,
	}
	child, err := agentrun.PrepareSubagent(active.ctx, s.runDependencies, agentrun.SubagentInput{
		Task:         input,
		AgentID:      agentID,
		ParentRunID:  parent.ID(),
		ParentCallID: callID,
		Definition:   definition,
		Model:        parent.Model(),
		Owner:        parent.Session,
	})
	if err != nil {
		if child != nil {
			saveErr := child.Settle(context.WithoutCancel(ctx), sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, "")
			err = errors.Join(err, saveErr)
		}
		s.releaseFailedChild(active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	child.BindParent(parent)
	initial := child.Snapshot()
	parent.RegisterSubagent(agentrun.SubagentHandle{
		Result:  initial,
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
		CreatedAt:     child.CreatedAt(),
		Run:           initial,
		Entries:       child.Entries(),
		ParentAgentID: parent.AgentID(),
		ParentRunID:   parent.ID(),
		CallID:        callID,
	}})
	s.mu.Lock()
	err = s.launchLocked(child, active)
	s.mu.Unlock()
	if err != nil {
		saveErr := child.Settle(context.WithoutCancel(ctx), sessionentry.OutcomeInterrupted, sessionentry.RunFailureCancelled, "")
		err = errors.Join(err, saveErr)
		s.releaseFailedChild(active)
		stopParent()
		cancelTimeout()
		return agentrun.SubagentStatus{}, err
	}
	return agentrun.SubagentStatus{AgentID: agentID, Run: initial}, nil
}
func (s *Coordinator) releaseFailedChild(active *activeRun) {
	close(active.ready)
	close(active.done)
	active.cancel(nil)
}
