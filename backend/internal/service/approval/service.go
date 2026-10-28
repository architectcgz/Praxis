// Package approval 负责审批模式配置与继承关系的查询和变更。
package approval

import (
	"context"
	"praxis/internal/clock"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// Dependencies 包含会话审批配置的持久化能力和只读配置快照来源。
type Dependencies struct {
	Config   ConfigSource
	Sessions repository.Session
	Clock    clock.Clock
}

// Service 负责会话审批模式配置；配置同步由快照来源管理。
type Service struct {
	config   ConfigSource
	sessions repository.Session
	clock    clock.Clock
}

func NewService(deps Dependencies) *Service {
	return &Service{
		config:   deps.Config,
		sessions: deps.Sessions,
		clock:    deps.Clock,
	}
}

// ApprovalSettings 返回业务层解析的继承关系和有效模式，与工具启动使用相同规则。
type ApprovalSettings struct {
	SessionID     identity.SessionID
	GlobalMode    security.ApprovalMode
	SessionMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

// GetApprovalMode 组合会话覆盖与当前全局模式，供配置界面展示。
func (s *Service) GetApprovalMode(ctx context.Context, sessionID identity.SessionID) (ApprovalSettings, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(session), nil
}

// SetApprovalMode 持久化会话覆盖并返回提交后的有效模式；写入失败不返回成功状态。
func (s *Service) SetApprovalMode(ctx context.Context, sessionID identity.SessionID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	session, err := s.sessions.SetApprovalMode(ctx, sessionID, mode, s.clock.Now().UTC())
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(session), nil
}

func (s *Service) approvalSettings(session sessionmodel.Session) ApprovalSettings {
	global := s.config.ApprovalConfig().GlobalMode
	return ApprovalSettings{
		SessionID:     session.ID,
		GlobalMode:    global,
		SessionMode:   session.ApprovalMode,
		EffectiveMode: security.EffectiveApprovalMode(global, session.ApprovalMode),
	}
}
