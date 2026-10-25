package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// ValidateDefinitions 要求默认定义存在，并禁止移除已有定义；角色属性允许重载。
// 首次加载时 current 为空，只检查候选定义的业务约束。
func ValidateDefinitions(current, candidate []agentmodel.AgentDefinition) error {
	definitions := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(candidate))
	for _, definition := range candidate {
		definitions[definition.ID] = definition
	}
	_, ok := definitions[agentmodel.DefinitionPrimary]
	if !ok {
		return fmt.Errorf("agent definition %q is required", agentmodel.DefinitionPrimary)
	}
	for _, previous := range current {
		_, ok := definitions[previous.ID]
		if !ok {
			return fmt.Errorf("loaded agent definition %q cannot be removed", previous.ID)
		}
	}
	return nil
}

// GetOrCreateMainAgent 按 Session 的身份和定义引用查找或创建主实例，不持久化 Agent。
// 并发创建由 Session 登记引用时合并，所有调用方最终取得同一指针。
func (s *Service) GetOrCreateMainAgent(ctx context.Context, sessionID identity.SessionID) (*agentmodel.Agent, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	agent, err := s.agent(session.MainAgentID)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return nil, err
	}
	if _, err := s.definitions(session.AgentDefinitionID); err != nil {
		return nil, err
	}
	agent = agentmodel.NewAgent(session.MainAgentID, session.ID, session.AgentDefinitionID, agentmodel.KindMain, session.CreatedAt)
	return s.registerAgent(ctx, agent)
}

// RestoreAgents 在 Coordinator 恢复执行前重建实例；执行事实仅用于这一次初始化。
// 未开始的输入和进程内控制意图不恢复，恢复失败时不开放业务入口。
func (s *Service) RestoreAgents(ctx context.Context) error {
	sessions, err := s.sessions.List(ctx, 0)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := s.GetOrCreateMainAgent(ctx, session.ID); err != nil {
			return fmt.Errorf("restore main agent in session %s: %w", session.ID, err)
		}
		executions, err := s.executionRecords.ListBySession(ctx, session.ID)
		if err != nil {
			return err
		}
		// 每个实例只恢复一次；活动执行优先，避免同一时间戳的历史排序覆盖当前归属。
		latest := make(map[identity.AgentID]agentexecution.Execution)
		for _, execution := range executions {
			previous, ok := latest[execution.AgentID]
			if !ok || execution.Active() || !previous.Active() && cmp.Or(execution.CreatedAt.Compare(previous.CreatedAt), cmp.Compare(execution.ID, previous.ID)) > 0 {
				latest[execution.AgentID] = execution
			}
		}
		for execution := range maps.Values(latest) {
			agent, err := s.agent(execution.AgentID)
			if errors.Is(err, errorcode.ErrNotFound) {
				if _, err := s.definitions(execution.Config.AgentDefinitionID); err != nil {
					return err
				}
				agent = agentmodel.NewAgent(execution.AgentID, session.ID, execution.Config.AgentDefinitionID, agentmodel.KindSubagent, execution.CreatedAt)
				agent, err = s.registerAgent(ctx, agent)
			}
			if err != nil {
				return err
			}
			if err := agent.BeginExecution(execution); err != nil {
				return fmt.Errorf("restore execution %s: %w", execution.ID, err)
			}
			agent.FinishExecution(execution)
		}
	}
	return nil
}
