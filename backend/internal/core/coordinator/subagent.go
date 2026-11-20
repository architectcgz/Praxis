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
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

type subagentAssignment struct {
	Task           string          `json:"task"`
	AllowedTools   []tool.ToolName `json:"allowedTools"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}

type subagentIDs struct {
	AgentID identity.AgentID `json:"agentId"`
	RunID   identity.RunID   `json:"runId"`
}

func (s *Coordinator) startSubagent(ctx context.Context, parent *agentrun.Run, parentActive *activeRun, assignment subagentAssignment, callID string) (subagentIDs, error) {
	catalog, err := tool.Restrict(parentActive.config.Tools, assignment.AllowedTools)
	if err != nil {
		return subagentIDs{}, err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || parentActive.ctx.Err() != nil {
		return subagentIDs{}, fmt.Errorf("parent execution is closing")
	}
	if parent.ActiveSubagents() >= 10 {
		return subagentIDs{}, fmt.Errorf("session subagent limit of 10 is reached")
	}
	agentID := identity.AgentID(s.runDependencies.IDs.New("agent"))
	active := newActiveRun(ctx, parentActive.session)
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
		return subagentIDs{}, err
	}
	if err := active.session.CreateSubagent(active.ctx, agentID); err != nil {
		s.releaseFailedChild(active)
		stopParent()
		cancelTimeout()
		if active.ctx.Err() != nil && errors.Is(err, active.ctx.Err()) {
			return subagentIDs{}, err
		}
		err = loop.NewFailure(loop.FailureStorage, err)
		parent.Cancel(err)
		return subagentIDs{}, err
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
		cancelTimeout()
		return subagentIDs{}, err
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
		cancelTimeout()
		return subagentIDs{}, err
	}
	return subagentIDs{AgentID: agentID, RunID: child.ID()}, nil
}
func (s *Coordinator) releaseFailedChild(active *activeRun) {
	close(active.done)
	active.cancel(nil)
}
