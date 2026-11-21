package coordinator

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/tool"
	subagenttool "praxis/internal/core/tool/subagent"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

type mainSubagentRuntime struct {
	coordinator *Coordinator
	run         *agentrun.Run
	active      *activeRun
}

func (m mainSubagentRuntime) StartSubagent(ctx context.Context, callID string, assignment subagenttool.Assignment) (subagenttool.StartResult, error) {
	return m.coordinator.startSubagent(ctx, m.run, m.active, assignment, callID)
}

func (m mainSubagentRuntime) WaitSubagents(ctx context.Context, ids []identity.AgentID, all bool) (any, error) {
	return m.run.WaitSubagents(ctx, ids, all)
}

func (m mainSubagentRuntime) SendToSubagent(ctx context.Context, id identity.AgentID, message string) (string, error) {
	return m.run.SendToSubagent(ctx, id, message)
}

func (m mainSubagentRuntime) CancelSubagent(id identity.AgentID) error {
	return m.run.CancelSubagent(id)
}

func (m mainSubagentRuntime) BeginWaiting() { m.run.BeginWaiting() }
func (m mainSubagentRuntime) EndWaiting()   { m.run.EndWaiting() }

func (s *Coordinator) startSubagent(ctx context.Context, parent *agentrun.Run, parentActive *activeRun, assignment subagenttool.Assignment, callID string) (subagenttool.StartResult, error) {
	catalog, err := tool.Restrict(parentActive.config.Tools, assignment.AllowedTools)
	if err != nil {
		return subagenttool.StartResult{}, err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || parentActive.ctx.Err() != nil {
		return subagenttool.StartResult{}, fmt.Errorf("parent execution is closing")
	}
	agentID := identity.AgentID(s.runDependencies.IDs.New("agent"))
	if !parent.ReserveSubagent(agentID, 10) {
		return subagenttool.StartResult{}, fmt.Errorf("session subagent limit of 10 is reached")
	}
	active := newActiveRun(ctx, parentActive.session)
	config := *parentActive.config
	config.Tools = catalog
	active.config = &config
	active.parent = parent
	childCtx, cancelTimeout := context.WithTimeout(parentActive.ctx, time.Duration(assignment.TimeoutSeconds)*time.Second)
	stopParent := context.AfterFunc(childCtx, func() { active.cancel(context.Cause(childCtx)) })
	created := false
	prepared := false
	launched := false
	defer func() {
		if launched {
			return
		}
		parent.ReleaseSubagent(agentID)
		if created && !prepared {
			_ = active.session.RemoveSubagent(context.WithoutCancel(ctx), agentID)
		}
		s.releaseFailedChild(active)
		stopParent()
		cancelTimeout()
	}()

	definition, err := config.Definition(agentmodel.DefinitionDelegate)
	if err != nil {
		return subagenttool.StartResult{}, err
	}
	if err := active.session.CreateSubagent(active.ctx, agentID); err != nil {
		if active.ctx.Err() != nil && errors.Is(err, active.ctx.Err()) {
			return subagenttool.StartResult{}, err
		}
		err = errorcode.ErrStorage.Wrap(err)
		parent.Cancel(err)
		return subagenttool.StartResult{}, err
	}
	created = true

	child := agentrun.New(s.runDependencies, agentrun.Config{
		ParentCallID: callID,
		Definition:   definition,
		Model:        parent.Model(),
	}, active.session.BindHistory(agentID))
	prepared, err = child.Prepare(active.ctx, assignment.Task)
	if err != nil {
		if prepared {
			if saveErr := child.SettlePreparation(active.ctx, err); saveErr != nil {
				err = errorcode.ErrStorage.Wrap(errors.Join(err, saveErr))
			}
		}
		if errors.Is(err, errorcode.ErrStorage) {
			parent.Cancel(err)
		}
		return subagenttool.StartResult{}, err
	}
	child.BindParent(parent)
	parent.AttachSubagent(agentrun.SubagentHandle{
		AgentID: agentID,
		Run:     child,
		Cancel:  active.cancel,
		Done:    active.done,
	})
	active.onDone = func() error {
		defer stopParent()
		defer cancelTimeout()
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
		saveErr := child.Settle(context.WithoutCancel(ctx), entry.OutcomeInterrupted, entry.RunFailureCancelled, "")
		return subagenttool.StartResult{}, errors.Join(errCoordinatorClosed, saveErr)
	}
	child.BindCancellation(active.cancel)
	s.running.Add(1)
	s.mu.Unlock()
	launched = true
	s.loop.Emitter.Emit(coreevent.Event{
		Source: child.EventSource(""),
		Payload: coreevent.SubagentStarted{
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
	return subagenttool.StartResult{AgentID: agentID, RunID: child.ID()}, nil
}
func (s *Coordinator) releaseFailedChild(active *activeRun) {
	close(active.done)
	active.cancel(nil)
}
