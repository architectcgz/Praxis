// Package approval 负责审批模式配置与会话已加载模式的查询。
package approval

import (
	"context"
	"praxis/internal/clock"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

type Dependencies struct {
	Sessions *sessionservice.Service
	Records  repository.Session
	Clock    clock.Clock
}

type Service struct {
	sessions *sessionservice.Service
	records  repository.Session
	clock    clock.Clock
}

func NewService(deps Dependencies) *Service {
	return &Service{
		sessions: deps.Sessions,
		records:  deps.Records,
		clock:    deps.Clock,
	}
}

// ApprovalSettings 区分已保存的会话覆盖和已加载的有效模式。
type ApprovalSettings struct {
	SessionID     identity.SessionID
	GlobalMode    security.ApprovalMode
	SessionMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

func (s *Service) GetApprovalMode(ctx context.Context, id identity.SessionID) (ApprovalSettings, error) {
	info, err := s.records.Get(ctx, id)
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(ctx, info)
}

// SetApprovalMode 只持久化会话覆盖；当前有效模式在显式 reload 成功后才更新。
func (s *Service) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	if _, err := s.sessions.Configuration(ctx, id); err != nil {
		return ApprovalSettings{}, err
	}
	info, err := s.records.SetApprovalMode(ctx, id, mode, s.clock.Now().UTC())
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(ctx, info)
}

func (s *Service) approvalSettings(ctx context.Context, info sessionmodel.SessionInfo) (ApprovalSettings, error) {
	config, err := s.sessions.Configuration(ctx, info.ID)
	if err != nil {
		return ApprovalSettings{}, err
	}
	return ApprovalSettings{
		SessionID:     info.ID,
		GlobalMode:    config.GlobalApprovalMode,
		SessionMode:   info.ApprovalMode,
		EffectiveMode: config.ApprovalMode,
	}, nil
}
