package bindings

import (
	"fmt"
	"praxis/internal/core/security"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	approvalservice "praxis/internal/service/approval"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

// GetProjectApprovalMode 读取项目授权选择；空或非 canonical 项目身份返回校验错误。
func (b *ProjectBindings) GetProjectApprovalMode(projectID string) (dto.ProjectApprovalMode, error) {
	if err := validation.ValidateProjectID(projectID); err != nil {
		return dto.ProjectApprovalMode{}, publicInputError(b.host, "ProjectBindings.GetProjectApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	value, err := services.Approvals.GetApprovalMode(ctx, identity.ProjectID(projectID))
	if err != nil {
		return dto.ProjectApprovalMode{}, publicError(b.host, "ProjectBindings.GetProjectApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

// SetProjectApprovalMode 保存项目授权选择；null 恢复继承，不影响已经 Started 的工具。
func (b *ProjectBindings) SetProjectApprovalMode(wire dto.SetProjectApprovalModeRequest) (dto.ProjectApprovalMode, error) {
	if err := validation.ValidateProjectID(wire.ProjectID); err != nil {
		return dto.ProjectApprovalMode{}, publicInputError(b.host, "ProjectBindings.SetProjectApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	if strings.ContainsAny(wire.ProjectID, "\x00\r\n") {
		err := fmt.Errorf("projectId: canonical project identity is required: %w", errorcode.ErrInvalidValue)
		return dto.ProjectApprovalMode{}, publicInputError(b.host, "ProjectBindings.SetProjectApprovalMode.request", err)
	}
	var mode *security.ApprovalMode
	if wire.Mode != nil {
		parsed := security.ApprovalMode(strings.TrimSpace(*wire.Mode))
		if !parsed.ValidDefault() {
			err := fmt.Errorf("approvalMode: default approval mode must be ask, auto or yolo: %w", errorcode.ErrInvalidValue)
			return dto.ProjectApprovalMode{}, publicInputError(b.host, "ProjectBindings.SetProjectApprovalMode.request", err)
		}
		mode = &parsed
	}
	value, err := services.Approvals.SetApprovalMode(ctx, identity.ProjectID(wire.ProjectID), mode)
	if err != nil {
		return dto.ProjectApprovalMode{}, publicError(b.host, "ProjectBindings.SetProjectApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

func approvalModeDTO(value approvalservice.ApprovalSettings) dto.ProjectApprovalMode {
	result := dto.ProjectApprovalMode{
		ProjectID:     value.ProjectID.String(),
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.ProjectMode != nil {
		mode := string(*value.ProjectMode)
		result.ProjectMode = &mode
	}
	return result
}
