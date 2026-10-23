package toolexecution

import (
	"encoding/json"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
)

type ApprovalCatalog interface {
	Get(tool.ToolName) (tool.Tool, bool)
	DecideCommand(string) security.ApprovalDecision
}

// commandTool 提供已准备参数中的待审批命令，空字符串表示不应用命令规则。
type commandTool interface {
	ApprovalCommand(json.RawMessage) (string, error)
}

func MatchApprovalRule(catalog ApprovalCatalog, name tool.ToolName, args json.RawMessage) (security.ApprovalDecision, error) {
	registered, enabled := catalog.Get(name)
	if !enabled {
		return security.ApprovalDecision{Action: security.ApprovalDeny, RuleID: "tool-disabled"}, nil
	}
	if source, ok := registered.(commandTool); ok {
		command, err := source.ApprovalCommand(args)
		if err != nil {
			return security.ApprovalDecision{}, err
		}
		if command != "" {
			return catalog.DecideCommand(command), nil
		}
	}
	return security.ApprovalDecision{}, nil
}
