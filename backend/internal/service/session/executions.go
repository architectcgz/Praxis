package session

import (
	"cmp"
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

var _ sessionmodel.ExecutionHistory = (*Service)(nil)

func (s *Service) GetExecution(ctx context.Context, sessionID identity.SessionID, id identity.AgentExecutionID) (agentexecution.ExecutionRecord, error) {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return agentexecution.ExecutionRecord{}, err
	}
	defer h.mu.Unlock()
	index := h.state.executionIndex(id)
	if index < 0 {
		return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
	}
	return cloneExecution(h.state.executions[index]), nil
}

func (s *Service) SaveExecution(ctx context.Context, value agentexecution.ExecutionRecord) error {
	h, err := s.lockHistory(ctx, value.SessionID)
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	return h.saveExecutionLocked(ctx, value)
}

func (s *Service) FindExecutionByRequest(ctx context.Context, agentID identity.AgentID, request identity.RequestID) (agentexecution.ExecutionRecord, error) {
	h, err := s.lockHistory(ctx, identity.SessionID(agentID))
	if err != nil {
		return agentexecution.ExecutionRecord{}, err
	}
	defer h.mu.Unlock()
	var found *agentexecution.ExecutionRecord
	for _, value := range h.state.executions {
		if value.AgentID != agentID || value.RequestID != request {
			continue
		}
		if found != nil {
			return agentexecution.ExecutionRecord{}, errorcode.ErrRequestConflict
		}
		found = new(cloneExecution(value))
	}
	if found == nil {
		return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
	}
	return *found, nil
}

func (s *Service) ListAgentExecutions(ctx context.Context, agentID identity.AgentID, limit int) ([]agentexecution.ExecutionRecord, error) {
	h, err := s.lockHistory(ctx, identity.SessionID(agentID))
	if err != nil {
		if errors.Is(err, errorcode.ErrNotFound) {
			return []agentexecution.ExecutionRecord{}, nil
		}
		return nil, err
	}
	defer h.mu.Unlock()
	values := make([]agentexecution.ExecutionRecord, 0)
	for _, value := range h.state.executions {
		if value.AgentID == agentID {
			values = append(values, cloneExecution(value))
		}
	}
	return capped(sorted(values, func(a, b agentexecution.ExecutionRecord) int {
		return -executionOrder(a, b)
	}), limit), nil
}

func (h *sessionHistory) saveExecutionLocked(ctx context.Context, value agentexecution.ExecutionRecord) error {
	value = cloneExecution(value)
	// 派生展示字段不属于持久化记录，也不留在会话历史状态中。
	value.Progress.OutputBytes, value.Progress.ToolCalls = 0, 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	if err := h.appendEventLocked(ctx, "agent_execution.saved", "agent_execution", value.ID.String(), value); err != nil {
		return err
	}
	h.state.setExecution(value)
	return nil
}

func cloneExecution(value agentexecution.ExecutionRecord) agentexecution.ExecutionRecord {
	value.Config = value.Config.Clone()
	return value
}

func executionOrder(a, b agentexecution.ExecutionRecord) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}
