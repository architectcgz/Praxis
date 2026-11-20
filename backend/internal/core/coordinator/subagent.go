package coordinator

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/loop"
	"praxis/internal/core/tool/subagent"
	"praxis/internal/identity"
)

func (s *Coordinator) startSubagent(ctx context.Context, parent *agentrun.Run, parentActive *activeRun, assignment subagent.Assignment, callID string) (subagent.IDs, error) {
	allowedTools, err := parentActive.allowedTools.Restrict(assignment.AllowedTools)
	if err != nil {
		return subagent.IDs{}, err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || parentActive.ctx.Err() != nil {
		return subagent.IDs{}, fmt.Errorf("parent execution is closing")
	}
	if parent.ActiveSubagents() >= 10 {
		return subagent.IDs{}, fmt.Errorf("session subagent limit of 10 is reached")
	}
	agentID := identity.AgentID(s.runDependencies.IDs.New("agent"))
	active := newActiveRun(ctx, parentActive.session)
	active.config = parentActive.config
	active.allowedTools = allowedTools
	active.parent = parent
	stopParent := context.AfterFunc(parentActive.ctx, func() { active.cancel(context.Cause(parentActive.ctx)) })
	definition, err := active.config.Definition(agentmodel.DefinitionDelegate)
	if err != nil {
		s.releaseFailedChild(active)
		stopParent()
		return subagent.IDs{}, err
	}
	if err := active.session.CreateSubagent(active.ctx, agentID); err != nil {
		s.releaseFailedChild(active)
		stopParent()
		if active.ctx.Err() != nil && errors.Is(err, active.ctx.Err()) {
			return subagent.IDs{}, err
		}
		err = loop.NewFailure(loop.FailureStorage, err)
		parent.Cancel(err)
		return subagent.IDs{}, err
	}
	child := agentrun.New(s.runDependencies, agentrun.Config{
		ParentCallID: callID,
		Definition:   definition,
		Model:        parent.Model(),
	}, active.session.BindHistory(agentID))
	prepared, err := child.Prepare(active.ctx, assignment.Task)
	if err != nil {
		if prepared {
			if saveErr := child.SettlePreparation(active.ctx, err); saveErr != nil {
				err = loop.NewFailure(loop.FailureStorage, errors.Join(err, saveErr))
			}
		}
		if loop.FailureClassification(err) == loop.FailureStorage {
			parent.Cancel(err)
		}
		s.releaseFailedChild(active)
		stopParent()
		return subagent.IDs{}, err
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
		if !child.TerminalSaved() {
			err := loop.NewFailure(loop.FailureStorage, fmt.Errorf("subagent %s did not save its terminal result", agentID))
			parent.Cancel(err)
			return err
		}
		return parent.AcceptSubagentResult(context.WithoutCancel(parentActive.ctx), child.ResultForParent())
	}
	s.loop.Emitter.Emit(coreevent.Event{Source: child.EventSource(""), Payload: coreevent.SubagentStarted{
		Name:          definition.Name,
		CreatedAt:     child.CreatedAt(),
		Entries:       child.Entries(),
		ParentAgentID: parent.AgentID(),
		ParentRunID:   parent.ID(),
		CallID:        callID,
	}})
	s.mu.Lock()
	err = s.launchLocked(child, active)
	s.mu.Unlock()
	if err != nil {
		saveErr := child.Settle(context.WithoutCancel(ctx), entry.OutcomeInterrupted, entry.RunFailureCancelled, "")
		err = errors.Join(err, saveErr)
		s.releaseFailedChild(active)
		stopParent()
		return subagent.IDs{}, err
	}
	return subagent.IDs{AgentID: agentID, RunID: child.ID()}, nil
}
func (s *Coordinator) releaseFailedChild(active *activeRun) {
	close(active.done)
	active.cancel(nil)
}
