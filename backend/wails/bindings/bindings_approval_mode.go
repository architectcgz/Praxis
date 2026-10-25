package bindings

import (
	"praxis/internal/core/security"
	"praxis/internal/identity"
	approvalservice "praxis/internal/service/approval"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

// GetProjectApprovalMode 读取项目授权选择；空或非 canonical 项目身份返回校验错误。
func (b *ProjectBindings) GetProjectApprovalMode(projectID string) (dto.ProjectApprovalMode, error) {
	if err := validation.Value("projectId", projectID, "project_id"); err != nil {
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
	if wire.Mode != nil {
		wire.Mode = new(strings.TrimSpace(*wire.Mode))
	}
	if err := validation.Struct(wire); err != nil {
		return dto.ProjectApprovalMode{}, publicInputError(b.host, "ProjectBindings.SetProjectApprovalMode.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ProjectApprovalMode{}, err
	}
	var mode *security.ApprovalMode
	if wire.Mode != nil {
		mode = new(security.ApprovalMode(*wire.Mode))
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
