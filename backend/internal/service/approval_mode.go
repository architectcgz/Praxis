package service

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/request"
	applicationproject "praxis/internal/service/project"
)

// ProjectApprovalMode 返回项目选择及后端解析的有效授权模式。
type ProjectApprovalMode struct {
	ProjectID     contracts.ProjectID
	GlobalMode    string
	ProjectMode   *string
	EffectiveMode string
}

// GetProjectApprovalMode 读取项目覆盖和全局继承结果，不创建 Session 级授权配置。
func (s *Services) GetProjectApprovalMode(ctx context.Context, projectID contracts.ProjectID) (ProjectApprovalMode, error) {
	value, err := s.projects.GetApprovalMode(ctx, projectID)
	return projectApprovalMode(value), err
}

// SetProjectApprovalMode 保存已由 request 边界校验的项目选择，失败时不向前端返回成功状态。
func (s *Services) SetProjectApprovalMode(ctx context.Context, value request.SetProjectApprovalMode) (ProjectApprovalMode, error) {
	result, err := s.projects.SetApprovalMode(ctx, value)
	return projectApprovalMode(result), err
}

func projectApprovalMode(value applicationproject.ApprovalSettings) ProjectApprovalMode {
	result := ProjectApprovalMode{
		ProjectID:     value.ProjectID,
		GlobalMode:    string(value.GlobalMode),
		EffectiveMode: string(value.EffectiveMode),
	}
	if value.ProjectMode != nil {
		mode := string(*value.ProjectMode)
		result.ProjectMode = &mode
	}
	return result
}
