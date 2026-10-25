package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
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

// RunSubagent 由委派工具的业务装配调用；委派必须绑定仍活跃的父调用和已注册子实例。
// 返回值由父工具路径接纳，子执行不能直接修改父执行的历史或模型上下文。
func (s *Coordinator) RunSubagent(ctx context.Context, parent ToolControl, agentID identity.AgentID, prompt string) (SubagentResult, error) {
	if err := s.enter(); err != nil {
		return SubagentResult{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	parentActive := s.executions[parent.AgentID]
	if s.closed || parentActive == nil || parentActive.executionID != parent.ExecutionID || parentActive.ending || parentActive.run == nil || s.executions[agentID] != nil {
		s.mu.Unlock()
		return SubagentResult{}, errorcode.ErrRequestConflict
	}
	parentRun := parentActive.run
	parentRecord := parentRun.Snapshot()
	active := newActiveExecution(ctx)
	s.executions[agentID] = active
	s.mu.Unlock()
	stopCaller := context.AfterFunc(ctx, func() { active.cancel(context.Cause(ctx)) })
	stopParent := context.AfterFunc(parentActive.ctx, func() { active.cancel(context.Cause(parentActive.ctx)) })
	defer stopCaller()
	defer stopParent()
	task := sessionmodel.Task{
		ID:        identity.TaskID((identity.SecureIDGenerator{}).New("task")),
		SessionID: parentRecord.SessionID,
		Prompt:    prompt,
	}
	run, _, err := s.operations.PrepareTask(active.ctx, task, agentID, parentRecord.Config.Model)
	if err != nil {
		s.mu.Lock()
		delete(s.executions, agentID)
		close(active.ready)
		active.cancel(nil)
		s.mu.Unlock()
		return SubagentResult{}, err
	}
	s.mu.Lock()
	active.run, active.executionID = run, run.Snapshot().ID
	close(active.ready)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.executions, agentID)
		active.run = nil
		active.cancel(nil)
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
