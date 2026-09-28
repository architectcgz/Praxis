package agent

import (
	"praxis/internal/contracts"

	"slices"
	"strings"
)

const (
	DefinitionPrimary  contracts.AgentDefinitionID = "primary"
	DefinitionDelegate contracts.AgentDefinitionID = "delegate"
	DefinitionAdvisor  contracts.AgentDefinitionID = "advisor"
	DefinitionCurator  contracts.AgentDefinitionID = "curator"
)

// AgentDefinition 描述可跨 Session 复用的 Agent 身份与默认工具集合。
type AgentDefinition struct {
	ID           contracts.AgentDefinitionID
	Profile      contracts.AgentProfile
	Instructions string
	AllowedTools []contracts.ToolName
	Revision     string
}

// Validate 校验定义身份、AGENT.md 内容和版本摘要。
func (d AgentDefinition) Validate() error {
	if !d.ID.Valid() {
		return contracts.InvalidValue("agentDefinition.id", "definition id is invalid")
	}
	if !d.Profile.Valid() {
		return contracts.InvalidValue("agentDefinition.profile", "unknown agent profile")
	}
	if strings.TrimSpace(d.Instructions) == "" {
		return contracts.InvalidValue("agentDefinition.instructions", "AGENT.md is required")
	}
	if strings.TrimSpace(d.Revision) == "" {
		return contracts.InvalidValue("agentDefinition.revision", "revision is required")
	}
	seen := make(map[contracts.ToolName]struct{}, len(d.AllowedTools))
	for _, tool := range d.AllowedTools {
		if !tool.Valid() {
			return contracts.InvalidValue("agentDefinition.allowedTools", "unknown tool")
		}
		if _, ok := seen[tool]; ok {
			return contracts.InvalidValue("agentDefinition.allowedTools", "duplicate tool")
		}
		seen[tool] = struct{}{}
	}
	return nil
}

// Snapshot 返回不会共享工具切片底层存储的定义副本。
func (d AgentDefinition) Snapshot() AgentDefinition {
	d.AllowedTools = slices.Clone(d.AllowedTools)
	return d
}
