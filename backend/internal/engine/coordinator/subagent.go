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
	agentID := identity.NewAgentID()
	active := newActiveRun(ctx, parentActive.session, parentActive.subagents)
	config := *parentActive.config
	config.Tools = catalog
	active.config = &config
	s.mu.Lock()
	schedule := s.schedules[parent.SessionID()]
	if s.closed || schedule.closing || parentActive.ctx.Err() != nil {
		s.mu.Unlock()
		s.releaseFailedChild(active)
		return tools.SubagentIDs{}, fmt.Errorf("parent execution or session is closing")
	}
	err = active.subagents.ReserveSubagent(runmodel.SubagentHandle{
		AgentID: agentID,
		Cancel:  active.cancel,
		Done:    active.done,
	}, s.activeSubagents)
	s.mu.Unlock()
	if err != nil {
		s.releaseFailedChild(active)
		return tools.SubagentIDs{}, err
	}
	created := false
	prepared := false
	launched := false
	defer func() {
		if launched {
			return
		}
		if created && !prepared {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.runDependencies.SettleTimeout)
			if err := active.session.RemoveSubagent(cleanupCtx, agentID); err != nil {
				s.logger.Errorf("remove unprepared subagent %s: %v", agentID, err)
			}
			cancel()
		}
		s.releaseFailedChild(active)
		active.subagents.ReleaseSubagent(agentID)
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
		ParentRunID:  parent.ID(),
		ParentCallID: callID,
		Definition:   definition,
		Model:        parent.Model(),
		Subagents:    active.subagents,
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
	active.subagents.AttachSubagent(runmodel.SubagentHandle{
		AgentID: agentID,
		Run:     child,
		Cancel:  active.cancel,
		Done:    active.done,
	})
	active.onDone = func() error {
		if !child.TerminalSaved() {
			return errorcode.ErrStorage.Wrap(fmt.Errorf("subagent %s did not save its terminal result", agentID))
		}
		return active.subagents.AcceptSubagentResult(context.WithoutCancel(active.ctx), child.ResultForParent())
	}

	s.mu.Lock()
	switch {
	case s.closed:
		err = errCoordinatorClosed
	case schedule.closing:
		err = errorcode.ErrRequestConflict
	case active.ctx.Err() != nil:
		err = context.Cause(active.ctx)
	}
	if err != nil {
		s.mu.Unlock()
		saveErr := child.Settle(active.ctx, entry.OutcomeInterrupted, entry.RunFailureCancelled, "")
		return tools.SubagentIDs{}, errors.Join(err, saveErr)
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
	active.cancel(nil)
	close(active.done)
}
