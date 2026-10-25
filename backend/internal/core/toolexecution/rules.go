package toolexecution

import (
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
)

type ApprovalCatalog interface {
	Get(tool.ToolName) (tool.Tool, bool)
	DecideCommand(string) security.ApprovalDecision
}

// MatchApprovalRule 使用准备阶段提取的命令匹配规则，不从序列化参数中反向解析。
func MatchApprovalRule(catalog ApprovalCatalog, name tool.ToolName, command string) security.ApprovalDecision {
	if _, enabled := catalog.Get(name); !enabled {
		return security.ApprovalDecision{
			Action: security.ApprovalDeny,
			RuleID: "tool-disabled",
		}
	}
	if command != "" {
		return catalog.DecideCommand(command)
	}
	return security.ApprovalDecision{}
}
