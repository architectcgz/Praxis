package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"sync"
)

type ApprovalCatalog interface {
	Get(tool.ToolName) (tool.Tool, bool)
	DecideCommand(string) security.ApprovalDecision
}

// commandTool 提供已准备参数中的待审批命令，空字符串表示不应用命令规则。
type commandTool interface {
	ApprovalCommand(json.RawMessage) (string, error)
}

type ApprovalRules struct {
	ConfigMu    *sync.RWMutex
	Catalog     ApprovalCatalog
	DefaultMode func(context.Context, identity.SessionID) (security.ApprovalMode, error)
}

// Decide 用配置读锁取得同一配置快照，显式 deny 始终优先。
func (r ApprovalRules) Decide(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	if r.ConfigMu != nil {
		r.ConfigMu.RLock()
		defer r.ConfigMu.RUnlock()
	}
	return r.decide(ctx, call, args)
}

// decide 供已经持有配置读锁的启动临界区使用，避免嵌套 RWMutex 读锁。
func (r ApprovalRules) decide(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	registered, enabled := r.Catalog.Get(call.Name)
	if !enabled {
		return security.ApprovalDecision{Action: security.ApprovalDeny, RuleID: "tool-disabled"}, nil
	}
	var matched security.ApprovalDecision
	if source, ok := registered.(commandTool); ok {
		command, err := source.ApprovalCommand(args)
		if err != nil {
			return security.ApprovalDecision{}, err
		}
		if command != "" {
			matched = r.Catalog.DecideCommand(command)
		}
	}
	if matched.Action == security.ApprovalDeny {
		return matched, nil
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
