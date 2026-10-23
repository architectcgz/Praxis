package session

import (
	"context"
	"errors"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// ModelConfigResolver 只解析模型目录中已经构建的配置，不保存用户选择。
type ModelConfigResolver interface {
	ModelConfig(string, string, string) (modelapi.Config, error)
}

// SelectModel 只更新指定会话的选择；解析失败时保留原选择，不修改已有执行。
func (s *Service) SelectModel(ctx context.Context, sessionID identity.SessionID, choice sessionmodel.ModelChoice) error {
	if ctx == nil {
		return errors.New("model selection context is required")
	}
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	s.selectionMu.Lock()
	defer s.selectionMu.Unlock()
	// 与删除共享同一把锁，避免会话删除后又写回选择；锁内不调用模型或启动执行。
	if err := RequireWritable(ctx, s.sessions, sessionID); err != nil {
		return err
	}
	if choice == (sessionmodel.ModelChoice{}) {
		delete(s.modelSelections, sessionID)
		return nil
	}
	if choice.ProviderID == "" || choice.ModelID == "" {
		return modelconfig.ErrNotConfigured
	}
	config, err := s.modelConfigs.ModelConfig(choice.ProviderID, choice.ModelID, choice.ReasoningLevel)
	if err != nil {
		return fault.InvalidValue("modelSelection", err.Error())
	}
	s.modelSelections[sessionID] = sessionmodel.NewModelSelection(config)
	return nil
}

// SelectedModel 返回界面可见的选择，不暴露完整配置；未选择时返回空值。
func (s *Service) SelectedModel(ctx context.Context, sessionID identity.SessionID) (sessionmodel.ModelChoice, error) {
	if ctx == nil {
		return sessionmodel.ModelChoice{}, errors.New("selected model context is required")
	}
	s.selectionMu.RLock()
	defer s.selectionMu.RUnlock()
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	return s.modelSelections[sessionID].Choice(), nil
}

// CurrentModelConfig 只复制指定会话的选择，不回退其他会话或默认模型。
func (s *Service) CurrentModelConfig(ctx context.Context, sessionID identity.SessionID) (modelapi.Config, error) {
	if ctx == nil {
		return modelapi.Config{}, errors.New("session model configuration context is required")
	}
	s.selectionMu.RLock()
	defer s.selectionMu.RUnlock()
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return modelapi.Config{}, err
	}
	return s.modelSelections[sessionID].CurrentModelConfig()
}
