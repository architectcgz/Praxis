package coordinator

import (
	"context"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// SessionModels 提供会话级模型选择能力；具体状态由会话服务管理。
type SessionModels interface {
	CurrentModelConfig(context.Context, identity.SessionID) (modelapi.Config, error)
	SelectModel(context.Context, identity.SessionID, sessionmodel.ModelChoice) error
}

// SelectModel 委托会话服务更新选择，再尝试唤醒该会话的预约，不影响其他会话。
func (s *Coordinator) SelectModel(ctx context.Context, sessionID identity.SessionID, choice sessionmodel.ModelChoice) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.sessionModels.SelectModel(ctx, sessionID, choice); err != nil {
		return err
	}
	if choice.ModelID == "" {
		return nil
	}
	if err := s.scheduleSession(ctx, sessionID, true); err != nil {
		s.logger.Errorf("activate queued input after model selection session_id=%s error=%v", sessionID, err)
	}
	return nil
}
