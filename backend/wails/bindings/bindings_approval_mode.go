package bindings

import (
	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/internal/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// GetProjectApprovalMode 读取项目授权选择；空或非 canonical 项目身份返回校验错误。
func (b *ProjectBindings) GetProjectApprovalMode(projectID string) (dto.ProjectApprovalMode, error) {
	if err := validation.ValidateProjectID(projectID); err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	value, err := services.Projects.GetProjectApprovalMode(ctx, contracts.ProjectID(projectID))
	if err != nil {
		return dto.ProjectApprovalMode{}, publicError(b.runtime, "ProjectBindings.GetProjectApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

// SetProjectApprovalMode 保存项目授权选择；null 恢复继承，不影响已经 Started 的工具。
func (b *ProjectBindings) SetProjectApprovalMode(wire dto.SetProjectApprovalModeRequest) (dto.ProjectApprovalMode, error) {
	if err := validation.ValidateProjectID(wire.ProjectID); err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	canonical, err := request.NewSetProjectApprovalMode(contracts.ProjectID(wire.ProjectID), wire.Mode)
	if err != nil {
		return dto.ProjectApprovalMode{}, publicError(b.runtime, "ProjectBindings.SetProjectApprovalMode.request", err)
	}
	value, err := services.Projects.SetProjectApprovalMode(ctx, canonical)
	if err != nil {
		return dto.ProjectApprovalMode{}, publicError(b.runtime, "ProjectBindings.SetProjectApprovalMode", err)
	}
	return approvalModeDTO(value), nil
}

func approvalModeDTO(value service.ProjectApprovalMode) dto.ProjectApprovalMode {
	return dto.ProjectApprovalMode{
		ProjectID:     value.ProjectID.String(),
		GlobalMode:    value.GlobalMode,
		ProjectMode:   value.ProjectMode,
		EffectiveMode: value.EffectiveMode,
	}
}
