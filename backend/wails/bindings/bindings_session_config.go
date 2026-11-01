package bindings

import (
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// ListModels 返回指定会话已加载的模型目录，不暴露凭据。
func (b *SessionBindings) ListModels(sessionID string) ([]dto.ModelOption, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "SessionBindings.ListModels.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	models, err := services.Sessions.ModelOptions(ctx, identity.SessionID(sessionID))
	if err != nil {
		return nil, publicError(b.host, "SessionBindings.ListModels", err)
	}
	result := make([]dto.ModelOption, 0, len(models))
	for _, option := range models {
		result = append(result, dto.ModelOption{
			ProviderID:            option.ProviderID,
			ModelID:               option.ModelID,
			Label:                 option.Label,
			ProviderName:          option.ProviderName,
			ReasoningLevels:       option.ReasoningLevels,
			DefaultReasoningLevel: option.DefaultReasoningLevel,
		})
	}
	return result, nil
}

// ReloadConfig 只重新加载指定会话；执行中拒绝重载，加载失败保留原配置。
func (b *SessionBindings) ReloadConfig(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "SessionBindings.ReloadConfig.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.Commands.ReloadSessionConfig(ctx, identity.SessionID(sessionID)); err != nil {
		return publicError(b.host, "SessionBindings.ReloadConfig", err)
	}
	return nil
}
