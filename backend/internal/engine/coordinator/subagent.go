package coordinator

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/agent"
	runmodel "praxis/internal/engine/run"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/tools"
)

func (s *Coordinator) startSubagent(ctx context.Context, parent *runmodel.Run, parentActive *activeRun, assignment tools.SubagentAssignment, callID string) (tools.SubagentIDs, error) {
	catalog, err := tools.Restrict(parentActive.config.Tools, assignment.AllowedTools)
	if err != nil {
		return tools.SubagentIDs{}, err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || parentActive.ctx.Err() != nil {
		return tools.SubagentIDs{}, fmt.Errorf("parent execution is closing")
	}
	agentID := identity.NewAgentID()
	if err := parent.ReserveSubagent(agentID, s.activeSubagents); err != nil {
		return tools.SubagentIDs{}, err
	}
	active := newActiveRun(ctx, parentActive.session)
	config := *parentActive.config
	config.Tools = catalog
	active.config = &config
	active.parent = parent
	stopParent := context.AfterFunc(parentActive.ctx, func() { active.cancel(context.Cause(parentActive.ctx)) })
	created := false
	prepared := false
	launched := false
	defer func() {
		if launched {
			return
		}
		parent.ReleaseSubagent(agentID)
		if created && !prepared {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runDependencies.SettleTimeout)
			if err := active.session.RemoveSubagent(cleanupCtx, agentID); err != nil {
				s.logger.Errorf("remove unprepared subagent %s: %v", agentID, err)
			}
			cancel()
		}
		s.releaseFailedChild(active)
		stopParent()
	}()

	definition, err := config.Definition(agent.DefinitionDelegate)
	if err != nil {
		return tools.SubagentIDs{}, err
	}
	prepareCtx, cancelPrepare := context.WithTimeout(active.ctx, s.runDependencies.PrepareTimeout)
	defer cancelPrepare()
	if err := active.session.CreateSubagent(prepareCtx, agentID); err != nil {
		if active.ctx.Err() != nil && errors.Is(err, active.ctx.Err()) {
			return tools.SubagentIDs{}, err
		}
		err = errorcode.ErrStorage.Wrap(err)
		parent.Cancel(err)
		return tools.SubagentIDs{}, err
	}
	created = true

	child := runmodel.New(s.runDependencies, runmodel.Config{
		ParentCallID: callID,
		Definition:   definition,
		Model:        parent.Model(),
	}, active.session.BindHistory(agentID))
	prepared, err = child.Prepare(prepareCtx, assignment.Task)
	if err != nil {
		if prepared {
			if saveErr := child.SettlePreparation(active.ctx, err); saveErr != nil {
				err = errorcode.ErrStorage.Wrap(errors.Join(err, saveErr))
			}
		}
		if errors.Is(err, errorcode.ErrStorage) {
			parent.Cancel(err)
		}
		return tools.SubagentIDs{}, err
	}
	child.BindParent(parent)
	parent.AttachSubagent(runmodel.SubagentHandle{
		AgentID: agentID,
		Run:     child,
		Cancel:  active.cancel,
		Done:    active.done,
	})
	active.onDone = func() error {
		defer stopParent()
		if !child.TerminalSaved() {
			err := errorcode.ErrStorage.Wrap(fmt.Errorf("subagent %s did not save its terminal result", agentID))
			parent.Cancel(err)
			return err
		}
		return parent.AcceptSubagentResult(context.WithoutCancel(parentActive.ctx), child.ResultForParent())
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		saveErr := child.Settle(ctx, entry.OutcomeInterrupted, entry.RunFailureCancelled, "")
		return tools.SubagentIDs{}, errors.Join(errCoordinatorClosed, saveErr)
	}
	child.BindCancellation(active.cancel)
	s.running.Add(1)
	s.mu.Unlock()
	launched = true
	s.emitter.Emit(event.Event{
		Source: child.EventSource(""),
		Payload: event.SubagentStarted{
			Name:          definition.Name,
			CreatedAt:     child.CreatedAt(),
			Entries:       child.Entries(),
			ParentAgentID: parent.AgentID(),
			ParentRunID:   parent.ID(),
			CallID:        callID,
		},
	})
	go func() {
		defer s.running.Done()
		s.run(active.ctx, child, active)
	}()
	return tools.SubagentIDs{AgentID: agentID, RunID: child.ID()}, nil
}
func (s *Coordinator) releaseFailedChild(active *activeRun) {
	close(active.done)
	active.cancel(nil)
}
