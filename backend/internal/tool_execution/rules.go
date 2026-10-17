package toolexecution

import (
	"context"
	"encoding/json"
	"errors"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
	toolmodel "praxis/internal/core/tool_execution"
	toolcontracts "praxis/internal/tools/contracts"
)

// ApprovalCatalog 提供当前工具可用性及命令匹配，不授予整个工具的永久执行权限。
type ApprovalCatalog interface {
	Get(contracts.ToolName) (toolcontracts.Tool, bool)
	DecideCommand(string) security.ApprovalDecision
}

// ApprovalRules 将具体规则和项目默认动作解析为单次操作的最终动作。
// Decide 必须在与配置重载及项目设置相同的事务锁内调用。
type ApprovalRules struct {
	Catalog     ApprovalCatalog
	DefaultMode func(context.Context, contracts.SessionID) (security.ApprovalMode, error)
}

// Decide 优先拒绝禁用工具和 deny 命令，其他操作没有隐含的 allow 豁免。
func (r ApprovalRules) Decide(ctx context.Context, invocation toolmodel.Execution) (security.ApprovalDecision, error) {
	if _, enabled := r.Catalog.Get(invocation.Name); !enabled {
		return security.ApprovalDecision{Action: security.ApprovalDeny, RuleID: "tool-disabled"}, nil
	}
	var matched security.ApprovalDecision
	if invocation.Name == contracts.ToolBash {
		var arguments struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(invocation.NormalizedArguments, &arguments); err != nil || arguments.Command == "" {
			return security.ApprovalDecision{}, errors.New("normalized bash command is required")
		}
		matched = r.Catalog.DecideCommand(arguments.Command)
		if matched.Action == security.ApprovalDeny {
			return matched, nil
		}
	}
	mode, err := r.DefaultMode(ctx, invocation.SessionID)
	if err != nil {
		return security.ApprovalDecision{}, err
	}
	if !mode.ValidDefault() {
		return security.ApprovalDecision{}, errors.New("default approval mode must be ask, auto or yolo")
	}
	return security.ResolveApprovalDecision(mode, matched), nil
}
