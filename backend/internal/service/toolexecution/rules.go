package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/message"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

type ApprovalCatalog interface {
	Get(tool.ToolName) (tool.Tool, bool)
	DecideCommand(string) security.ApprovalDecision
}

type ApprovalRules struct {
	Catalog     ApprovalCatalog
	DefaultMode func(context.Context, identity.SessionID) (security.ApprovalMode, error)
}

// Decide 与配置重载共用事务边界，显式 deny 始终优先。
func (r ApprovalRules) Decide(ctx context.Context, call message.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	if _, enabled := r.Catalog.Get(call.Name); !enabled {
		return security.ApprovalDecision{Action: security.ApprovalDeny, RuleID: "tool-disabled"}, nil
	}
	var matched security.ApprovalDecision
	if call.Name == tool.ToolBash {
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &input); err != nil || input.Command == "" {
			return security.ApprovalDecision{}, errors.New("prepared bash command is required")
		}
		matched = r.Catalog.DecideCommand(input.Command)
		if matched.Action == security.ApprovalDeny {
			return matched, nil
		}
	}
	mode, err := r.DefaultMode(ctx, call.SessionID)
	if err != nil {
		return security.ApprovalDecision{}, err
	}
	if !mode.ValidDefault() {
		return security.ApprovalDecision{}, errors.New("default approval mode must be ask, auto or yolo")
	}
	return security.ResolveApprovalDecision(mode, matched), nil
}
