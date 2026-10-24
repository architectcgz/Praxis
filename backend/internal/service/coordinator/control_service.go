package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"time"
)

// ControlResult 只说明本次进程是否接受信号，不代表底层执行已经退出。
type ControlResult struct {
	AgentID           identity.AgentID
	TargetExecutionID identity.AgentExecutionID
	Applied           bool
}

// PauseAgent 只暂停身份匹配的当前执行；不匹配时跳过，原执行在后台退出并结算。
func (s *Coordinator) PauseAgent(ctx context.Context, agentID identity.AgentID, executionID identity.AgentExecutionID) (ControlResult, error) {
	return s.applyControl(ctx, agentID, executionID, agentmodel.AgentPaused, errExecutionPaused)
}

// StopAgent 中断明确指定的执行，保留尚未开始的输入。
func (s *Coordinator) StopAgent(ctx context.Context, agentID identity.AgentID, executionID identity.AgentExecutionID) (ControlResult, error) {
	return s.applyControl(ctx, agentID, executionID, agentmodel.AgentInterrupted, errExecutionStopped)
}

func (s *Coordinator) applyControl(ctx context.Context, agentID identity.AgentID, executionID identity.AgentExecutionID, state agentmodel.AgentState, cause error) (ControlResult, error) {
	if err := s.enter(ctx); err != nil {
		return ControlResult{}, err
	}
	defer s.running.Done()
	result := ControlResult{
		AgentID:           agentID,
		TargetExecutionID: executionID,
	}
	agent, err := s.agent(agentID)
	if err != nil {
		return result, err
	}
	result.Applied, err = s.cancelExecution(ctx, agentID, executionID, cause)
	if err == nil && result.Applied {
		agent.ApplyControl(executionID, state, time.Now())
	}
	return result, err
}
