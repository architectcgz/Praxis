package input

import (
	"context"
	"errors"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/service/agent"
)

// PrepareNext 在一次事务中读取队首、冻结执行输入、创建执行并标记预约派发。
// 事务失败释放本次预留；提交后崩溃由 Execution 恢复扫描接管。
func (s *Service) PrepareNext(ctx context.Context, agentID identity.AgentID) (agent.Execution, bool, error) {
	var built agent.Execution
	var admitted bool
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		owner, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if !owner.CanReadSessionContext() {
			return fault.New(fault.InvalidRequest, "queue requires the session main agent")
		}
		if owner.State != agent.AgentIdle && owner.State != agent.AgentWaiting {
			return nil
		}
		pending, err := s.queue.FindNextPendingByAgent(txCtx, owner.ID)
		if errors.Is(err, fault.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := pending.Prepare(); err != nil {
			return err
		}
		count, err := s.records.CountActiveBySession(txCtx, owner.SessionID)
		if err != nil || count != 0 {
			return err
		}
		config, err := s.buildConfig(txCtx, owner, pending.ProviderID, pending.ModelID, pending.ReasoningLevel)
		if err != nil {
			return err
		}
		selection, err := s.selectContext(txCtx, owner, config, pending.InputMessageID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		built = agent.NewExecution(identity.AgentExecutionID(s.ids.New("execution")), owner.SessionID, owner.ID, pending.RequestID, pending.InputMessageID, config, selection, at)
		built.SourceTaskID = pending.ID
		built.ProviderID, built.ModelID, built.ReasoningLevel = pending.ProviderID, pending.ModelID, pending.ReasoningLevel
		if err := owner.StartExecution(built.ID, at); err != nil {
			return err
		}
		if err := pending.MarkDispatched(built.ID, at); err != nil {
			return err
		}
		_, admitted, err = s.executions.Reserve(ctx, owner.ID, built.ID)
		if err != nil {
			return err
		}
		if err := s.records.Save(txCtx, built); err != nil {
			return err
		}
		if err := s.queue.Save(txCtx, pending); err != nil {
			return err
		}
		return s.agents.Save(txCtx, owner)
	})
	if err != nil {
		if admitted {
			s.executions.Release(agentID, built.ID)
		}
		return agent.Execution{}, false, err
	}
	return built, built.ID != "", nil
}
