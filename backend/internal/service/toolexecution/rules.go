package toolexecution

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/security"
	sessionentry "praxis/internal/core/session/entry"
	executionmodel "praxis/internal/core/toolexecution"
)

// DecideRule 在每次 ToolCall 开始授权时，根据本次调用所属的 Session 和 Project 读取有效审批配置。
// 全局模式和工具规则来自同一份只读快照；后续审批与启动沿用本次决定，不受配置变更影响。
func (s *Service) DecideRule(ctx context.Context, call sessionentry.ToolCall, args json.RawMessage) (security.ApprovalDecision, error) {
	config := s.deps.ApprovalConfig.ApprovalConfig()
	matched, err := executionmodel.MatchApprovalRule(config.Tools, call.Name, args)
	if err != nil {
		return security.ApprovalDecision{}, err
	}
	if matched.Action == security.ApprovalDeny {
		return matched, nil
	}
	session, err := s.deps.Sessions.Get(ctx, call.SessionID)
	if err != nil {
		return security.ApprovalDecision{}, err
	}
	project, err := s.deps.Projects.Get(ctx, session.ProjectID)
	if err != nil {
		return security.ApprovalDecision{}, err
	}
	mode := security.EffectiveApprovalMode(config.GlobalMode, project.ApprovalMode)
	if !mode.ValidDefault() {
		return security.ApprovalDecision{}, errors.New("effective approval mode must be ask, auto or yolo")
	}
	return security.ResolveApprovalDecision(mode, matched), nil
}
