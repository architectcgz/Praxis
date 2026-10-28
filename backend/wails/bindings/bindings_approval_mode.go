package bindings

import (
	"praxis/internal/core/security"
	"praxis/internal/identity"
	approvalservice "praxis/internal/service/approval"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

// GetSessionApprovalMode 读取会话授权选择；空或非 canonical 会话身份返回校验错误。
func (b *SessionBindings) GetSessionApprovalMode(sessionID string) (dto.SessionApprovalMode, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionApprovalMode{}, publicInputError(b.host, "SessionBindings.GetSessionApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionApprovalMode{}, err
	}
	value, err := services.Approvals.GetApprovalMode(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionApprovalMode{}, publicError(b.host, "SessionBindings.GetSessionApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

// SetSessionApprovalMode 保存会话授权选择；null 恢复继承，不影响已经开始的工具授权。
func (b *SessionBindings) SetSessionApprovalMode(wire dto.SetSessionApprovalModeRequest) (dto.SessionApprovalMode, error) {
	if wire.Mode != nil {
		wire.Mode = new(strings.TrimSpace(*wire.Mode))
	}
	if err := validation.Struct(wire); err != nil {
		return dto.SessionApprovalMode{}, publicInputError(b.host, "SessionBindings.SetSessionApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionApprovalMode{}, err
	}
	var mode *security.ApprovalMode
	if wire.Mode != nil {
		mode = new(security.ApprovalMode(*wire.Mode))
	}
	value, err := services.Approvals.SetApprovalMode(ctx, identity.SessionID(wire.SessionID), mode)
	if err != nil {
		return dto.SessionApprovalMode{}, publicError(b.host, "SessionBindings.SetSessionApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

func approvalModeDTO(value approvalservice.ApprovalSettings) dto.SessionApprovalMode {
	result := dto.SessionApprovalMode{
		SessionID:     value.SessionID.String(),
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.SessionMode != nil {
		result.SessionMode = new(string(*value.SessionMode))
	}
	return result
}
