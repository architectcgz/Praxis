package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"sync"
)

type ApprovalRules struct {
	ConfigMu    *sync.RWMutex
	Catalog     executionmodel.ApprovalCatalog
	DefaultMode func(context.Context, identity.SessionID) (security.ApprovalMode, error)
}

// decide 在一次配置读取中匹配当前内存规则。
func (r ApprovalRules) decide(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	matched, err := executionmodel.MatchApprovalRule(r.Catalog, call.Name, args)
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

// DecideRule 仅在 ToolCall 开始授权时读取一次规则。
func (s *Service) DecideRule(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	if mu := s.deps.Rules.ConfigMu; mu != nil {
		mu.RLock()
		defer mu.RUnlock()
	}
	return s.deps.Rules.decide(ctx, call, args)
}
