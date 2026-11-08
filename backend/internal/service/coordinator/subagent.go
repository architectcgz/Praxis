package coordinator

import (
	"context"
	"errors"
	agentrun "praxis/internal/core/agent/run"
	coreevent "praxis/internal/core/event"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	runservice "praxis/internal/service/agent/run"
)

// SubagentResult 只交接父调用需要的结果；完整执行事实保存在子代理日志。
type SubagentResult struct {
	RunID       identity.RunID
	Outcome     agentrun.RunOutcome
	FailureCode agentrun.RunFailureCode
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
	parentActive := s.activeRunLocked(parent.AgentID, parent.RunID)
	if s.closed || parentActive == nil || parentActive.ending || parentActive.run == nil {
		s.mu.Unlock()
		return SubagentResult{}, errorcode.ErrRequestConflict
	}
	parentRun := parentActive.run
	active := newActiveRun(ctx, parentActive.sessionID)
	active.config = parentActive.config
	active.branchID, active.bindingID = parentActive.branchID, parentActive.bindingID
	key := runKey{agentID, parentActive.branchID}
	if s.runs[key] != nil {
		s.mu.Unlock()
		return SubagentResult{}, errorcode.ErrRequestConflict
	}
	s.runs[key] = active
	s.mu.Unlock()
	stopCaller := context.AfterFunc(ctx, func() { active.cancel(context.Cause(ctx)) })
	stopParent := context.AfterFunc(parentActive.ctx, func() { active.cancel(context.Cause(parentActive.ctx)) })
	defer stopCaller()
	defer stopParent()
	parentRecord := parentRun.Snapshot()
	task := sessionmodel.Task{
		ID:        identity.TaskID((identity.SecureIDGenerator{}).New("task")),
		SessionID: parentRecord.SessionID,
		BranchID:  parentRecord.BranchID,
		Prompt:    prompt,
	}
	definition, err := active.config.Definition(definitionID)
	var run *agentrun.Run
	if err == nil {
		run, err = s.operations.PrepareSubagent(active.ctx, runservice.SubagentInput{
			Task:         task,
			AgentID:      agentID,
			ParentRunID:  parent.RunID,
			ParentCallID: parent.CallID,
			Definition:   definition,
			Model:        parentRecord.Config.Model,
			Owner:        parentRun.Session,
			BindingID:    parentActive.bindingID,
		})
	}
	if err != nil {
		s.mu.Lock()
		delete(s.runs, key)
		close(active.ready)
		active.cancel(nil)
		s.mu.Unlock()
		return SubagentResult{}, err
	}
	runID := run.Snapshot().ID
	s.mu.Lock()
	active.run, active.runID = run, runID
	close(active.ready)
	s.mu.Unlock()
	s.loop.Emitter.Emit(coreevent.Event{
		Source: coreevent.SourceForRun(run, ""),
		Payload: coreevent.SubagentStarted{
			Name:          definition.Name,
			Run:           run.Snapshot(),
			Entries:       run.Entries(),
			ParentAgentID: parent.AgentID,
			ParentRunID:   parent.RunID,
			CallID:        parent.CallID,
		},
	})
	defer func() {
		s.mu.Lock()
		delete(s.runs, key)
		active.run = nil
		active.cancel(nil)
		s.mu.Unlock()
		s.loop.Emitter.Emit(coreevent.Event{
			Source:  coreevent.SourceForRun(run, ""),
			Payload: coreevent.SubagentReleased{},
		})
	}()
	// 同步等待子路径和结算退出，调用方取消不能把结果交接变成脱离生命周期的后台提交。
	s.run(active.ctx, run, active)
	record := run.Snapshot()
	result := SubagentResult{
		RunID:       record.ID,
		Outcome:     record.Outcome,
		FailureCode: record.FailureCode,
	}
	if entry, found := run.Entry(record.Progress.FinalAssistantID); found {
		result.Output = entry.TextContent()
	}
	if record.Status != agentrun.RunEnded {
		return result, errors.New("subagent execution did not settle")
	}
	return result, nil
}
