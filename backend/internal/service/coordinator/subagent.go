package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// SubagentResult 只交接调用方需要的结果，不保存子执行的消息、计时或审核过程。
type SubagentResult struct {
	ExecutionID identity.AgentExecutionID
	Outcome     agentexecution.ExecutionOutcome
	FailureCode agentexecution.ExecutionFailureCode
	Output      string
}

// RunSubagent 由委派工具的业务装配调用；子执行按指定角色创建并持有自己的 Agent，且绑定活跃父调用。
// 返回值由父工具路径接纳，子执行不能直接修改父执行的历史或模型上下文。
func (s *Coordinator) RunSubagent(ctx context.Context, parent ToolControl, agentID identity.AgentID, definitionID identity.AgentDefinitionID, prompt string) (SubagentResult, error) {
	if err := s.enter(); err != nil {
		return SubagentResult{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	parentActive := s.activeExecutionLocked(parent.AgentID, parent.ExecutionID)
	if s.closed || parentActive == nil || parentActive.ending || parentActive.run == nil {
		s.mu.Unlock()
		return SubagentResult{}, errorcode.ErrRequestConflict
	}
	parentRun := parentActive.run
	parentRecord := parentRun.Snapshot()
	active := newActiveExecution(ctx, parentRecord.SessionID)
	active.config = parentActive.config
	active.branchID, active.bindingID = parentRecord.BranchID, parentActive.bindingID
	key := executionKey{agentID, parentRecord.BranchID}
	if s.executions[key] != nil {
		s.mu.Unlock()
		return SubagentResult{}, errorcode.ErrRequestConflict
	}
	s.executions[key] = active
	s.mu.Unlock()
	stopCaller := context.AfterFunc(ctx, func() { active.cancel(context.Cause(ctx)) })
	stopParent := context.AfterFunc(parentActive.ctx, func() { active.cancel(context.Cause(parentActive.ctx)) })
	defer stopCaller()
	defer stopParent()
	task := sessionmodel.Task{
		ID:        identity.TaskID((identity.SecureIDGenerator{}).New("task")),
		SessionID: parentRecord.SessionID,
		BranchID:  parentRecord.BranchID,
		Prompt:    prompt,
	}
	definition, err := active.config.Definition(definitionID)
	var run *agentexecution.Run
	if err == nil {
		run, _, err = s.operations.PrepareTask(active.ctx, task, agentID, definition, parentRecord.Config.Model, nil, parentActive.bindingID)
	}
	if err != nil {
		s.mu.Lock()
		delete(s.executions, key)
		close(active.ready)
		active.cancel(nil)
		s.mu.Unlock()
		return SubagentResult{}, err
	}
	s.mu.Lock()
	active.run, active.executionID = run, run.Snapshot().ID
	close(active.ready)
	s.mu.Unlock()
	s.loop.Emitter.Emit(coreevent.Event{
		Scope: coreevent.ScopeForRun(run, ""),
		Payload: coreevent.SubagentStarted{
			Name:              definition.Name,
			Execution:         run.Snapshot(),
			Entries:           run.Entries(),
			ParentAgentID:     parent.AgentID,
			ParentExecutionID: parent.ExecutionID,
			CallID:            parent.CallID,
		},
	})
	defer func() {
		s.mu.Lock()
		delete(s.executions, key)
		active.run = nil
		active.cancel(nil)
		s.mu.Unlock()
		s.loop.Emitter.Emit(coreevent.Event{
			Scope:   coreevent.ScopeForRun(run, ""),
			Payload: coreevent.SubagentReleased{},
		})
	}()
	// 同步等待子路径和结算退出，调用方取消不能把结果交接变成脱离生命周期的后台提交。
	s.run(active.ctx, run, active)
	record := run.Snapshot()
	result := SubagentResult{
		ExecutionID: record.ID,
		Outcome:     record.Outcome,
		FailureCode: record.FailureCode,
	}
	if entry, found := run.Entry(record.Progress.FinalAssistantID); found {
		result.Output = entry.TextContent()
	}
	if record.Status != agentexecution.ExecutionEnded {
		return result, errors.New("subagent execution did not settle")
	}
	return result, nil
}
