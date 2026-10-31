package execution

import (
	"context"
	providerapi "praxis/internal/core/provider"
	"time"
)

// RecordUsage 将用量交给会话确认归属并保存；观测失败不改变模型响应。
func (s *Service) RecordUsage(record providerapi.UsageRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.usageRecords.SaveUsage(ctx, record); err != nil {
		s.logger.Errorf("token usage save failed execution=%s turn=%s: %v", record.ExecutionID, record.TurnID, err)
	}
}
