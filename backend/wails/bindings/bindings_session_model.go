package bindings

import (
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

// SelectModel 只更新指定会话的选择；队列唤醒由 Coordinator 编排。
func (b *SessionBindings) SelectModel(wire dto.SelectModelRequest) error {
	if err := validation.ValidateSessionID(wire.SessionID); err != nil {
		return publicInputError(b.host, "SessionBindings.SelectModel.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	choice := sessionmodel.ModelChoice{
		ProviderID:     strings.TrimSpace(wire.ProviderID),
		ModelID:        strings.TrimSpace(wire.ModelID),
		ReasoningLevel: strings.TrimSpace(wire.ReasoningLevel),
	}
	if err := services.Commands.SelectModel(ctx, identity.SessionID(wire.SessionID), choice); err != nil {
		return publicError(b.host, "SessionBindings.SelectModel", err)
	}
	return nil
}

// GetModelSelection 返回会话自身的选择；查询不会设置模型或唤醒预约。
func (b *SessionBindings) GetModelSelection(sessionID string) (dto.ModelSelection, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return dto.ModelSelection{}, publicInputError(b.host, "SessionBindings.GetModelSelection.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ModelSelection{}, err
	}
	choice, err := services.Sessions.SelectedModel(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.ModelSelection{}, publicError(b.host, "SessionBindings.GetModelSelection", err)
	}
	return dto.ModelSelection{
		ProviderID:     choice.ProviderID,
		ModelID:        choice.ModelID,
		ReasoningLevel: choice.ReasoningLevel,
	}, nil
}
