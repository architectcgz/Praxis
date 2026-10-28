package execution

import (
	"context"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

// RecordUsage 在执行归属确认后保存本次请求的用量；观测失败不改变模型响应。
func (s *Service) RecordUsage(record providerapi.UsageRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.saveUsage(ctx, record); err != nil {
		s.logger.Errorf("token usage save failed execution=%s turn=%s: %v", record.ExecutionID, record.TurnID, err)
	}
}

func (s *Service) saveUsage(ctx context.Context, record providerapi.UsageRecord) error {
	execution, err := s.records.Get(ctx, identity.AgentExecutionID(record.ExecutionID))
	if err != nil {
		return err
	}
	if execution.SessionID.String() != record.SessionID || execution.AgentID.String() != record.AgentID {
		return errorcode.ErrRequestConflict
	}
	session, err := s.sessions.Get(ctx, execution.SessionID)
	if err != nil {
		return err
	}
	if session.MainAgentID != execution.AgentID {
		return errorcode.ErrRequestConflict
	}
	previous, err := s.usageRecords.ListSession(ctx, record.SessionID)
	if err != nil {
		return err
	}
	for _, prior := range previous {
		if prior.Key() == record.Key() && (prior.AgentID != record.AgentID || prior.ExecutionID != record.ExecutionID || !record.Usage.Includes(prior.Usage)) {
			return errorcode.ErrRequestConflict
		}
	}
	return s.usageRecords.Save(ctx, record)
}
