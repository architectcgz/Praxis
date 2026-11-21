package session

import (
	"context"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// ApprovalSettings 区分已保存的会话覆盖和已加载的有效模式。
type ApprovalSettings struct {
	SessionID     identity.SessionID
	GlobalMode    security.ApprovalMode
	SessionMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

func (s *Service) Configuration(ctx context.Context, id identity.SessionID) (*sessionmodel.Configuration, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Configuration(), nil
}

func (s *Service) ModelOptions(ctx context.Context, id identity.SessionID) ([]modelconfig.Option, error) {
	config, err := s.Configuration(ctx, id)
	if err != nil {
		return nil, err
	}
	return config.Models.ListModels(), nil
}

func (s *Service) SelectedModel(ctx context.Context, id identity.SessionID) (sessionmodel.ModelChoice, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	return owner.Choice(), nil
}

func (s *Service) SelectModel(ctx context.Context, id identity.SessionID, choice sessionmodel.ModelChoice) error {
	return s.core.SelectModel(ctx, id, choice)
}

func (s *Service) ReloadSessionConfig(ctx context.Context, id identity.SessionID) error {
	return s.core.ReloadSessionConfig(ctx, id)
}

func (s *Service) GetApprovalMode(ctx context.Context, id identity.SessionID) (ApprovalSettings, error) {
	info, err := s.sessions.Get(ctx, id)
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(ctx, info)
}

// SetApprovalMode 只持久化会话覆盖；当前有效模式在显式 reload 成功后才更新。
func (s *Service) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	if _, err := s.Configuration(ctx, id); err != nil {
		return ApprovalSettings{}, err
	}
	info, err := s.sessions.SetApprovalMode(ctx, id, mode, s.clock.Now().UTC())
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(ctx, info)
}

func (s *Service) approvalSettings(ctx context.Context, info sessionmodel.SessionInfo) (ApprovalSettings, error) {
	config, err := s.Configuration(ctx, info.ID)
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
