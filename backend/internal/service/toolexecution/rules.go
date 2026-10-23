package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/toolexecution/authorization"
	"praxis/internal/identity"
	"sync"
)

type ApprovalRules struct {
	ConfigMu    *sync.RWMutex
	Catalog     authorization.ApprovalCatalog
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
	matched, err := authorization.MatchApprovalRule(r.Catalog, call.Name, args)
	if err != nil {
		return security.ApprovalDecision{}, err
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
