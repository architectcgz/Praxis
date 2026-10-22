package agent

import (
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// ValidateDefinitions 校验 Primary 身份，并禁止移除已有定义或改变其 Profile。
// 首次加载时 current 为空，只检查候选定义的业务约束。
func ValidateDefinitions(current, candidate []agentmodel.AgentDefinition) error {
	definitions := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(candidate))
	for _, definition := range candidate {
		if definition.ID != agentmodel.DefinitionPrimary && definition.Profile == agentmodel.ProfilePrimary {
			return fmt.Errorf("agent definition %q cannot use primary profile", definition.ID)
		}
		definitions[definition.ID] = definition
	}
	primary, ok := definitions[agentmodel.DefinitionPrimary]
	if !ok {
		return fmt.Errorf("agent definition %q is required", agentmodel.DefinitionPrimary)
	}
	if primary.Profile != agentmodel.ProfilePrimary {
		return fmt.Errorf("agent definition %q must use primary profile, got %q", primary.ID, primary.Profile)
	}
	for _, previous := range current {
		definition, ok := definitions[previous.ID]
		if !ok {
			return fmt.Errorf("loaded agent definition %q cannot be removed", previous.ID)
		}
		if definition.Profile != previous.Profile {
			return fmt.Errorf("agent definition %q profile cannot change from %q to %q", previous.ID, previous.Profile, definition.Profile)
		}
	}
	return nil
}

// ValidateStoredDefinitions 在启动恢复前校验配置及所有已保存 Agent 的定义归属。
func ValidateStoredDefinitions(ctx context.Context, definitions []agentmodel.AgentDefinition, sessions repository.Session, agents repository.Agent) error {
	if err := ValidateDefinitions(nil, definitions); err != nil {
		return err
	}
	byID := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
	}
	storedSessions, err := sessions.List(ctx, 0)
	if err != nil {
		return fmt.Errorf("list sessions for agent definition validation: %w", err)
	}
	for _, session := range storedSessions {
		storedAgents, err := agents.ListBySession(ctx, session.ID, 0)
		if err != nil {
			return fmt.Errorf("list agents in session %q for definition validation: %w", session.ID, err)
		}
		for _, owner := range storedAgents {
			definition, ok := byID[owner.DefinitionID]
			if !ok {
				return fmt.Errorf("stored agent %q references missing agent definition %q", owner.ID, owner.DefinitionID)
			}
			if owner.Profile != definition.Profile {
				return fmt.Errorf("stored agent %q profile %q conflicts with agent definition %q profile %q", owner.ID, owner.Profile, definition.ID, definition.Profile)
			}
		}
	}
	return nil
}
