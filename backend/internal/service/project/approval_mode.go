package project

import (
	"context"

	"praxis/internal/contracts"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	"praxis/internal/request"
)

// ApprovalSettings 同时返回继承关系与实际生效模式，避免前端自行计算授权策略。
type ApprovalSettings struct {
	ProjectID     contracts.ProjectID
	GlobalMode    security.ApprovalMode
	ProjectMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

// GetApprovalMode 在与工具启动相同的事务边界读取项目与全局模式。
func (s *Service) GetApprovalMode(ctx context.Context, projectID contracts.ProjectID) (ApprovalSettings, error) {
	var result ApprovalSettings
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		project, err := s.projects.Get(txCtx, projectID)
		if err != nil {
			return err
		}
		result = s.approvalSettings(project)
		return nil
	})
	return result, err
}

// SetApprovalMode 持久化项目覆盖并返回提交后的有效模式；写入失败不返回成功状态。
func (s *Service) SetApprovalMode(ctx context.Context, value request.SetProjectApprovalMode) (ApprovalSettings, error) {
	var result ApprovalSettings
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		project, err := s.projects.Get(txCtx, value.ProjectID)
		if err != nil {
			return err
		}
		if project.ApprovalMode == nil && value.Mode == nil || project.ApprovalMode != nil && value.Mode != nil && *project.ApprovalMode == *value.Mode {
			result = s.approvalSettings(project)
			return nil
		}
		project.ApprovalMode = value.Mode
		project.UpdatedAt = s.clock.Now().UTC()
		if err := s.projects.Save(txCtx, project); err != nil {
			return err
		}
		result = s.approvalSettings(project)
		return nil
	})
	if err != nil {
		return ApprovalSettings{}, err
	}
	return result, nil
}

func (s *Service) approvalSettings(project projectmodel.Project) ApprovalSettings {
	global := s.globalApprovalMode()
	return ApprovalSettings{
		ProjectID:     project.ID,
		GlobalMode:    global,
		ProjectMode:   project.ApprovalMode,
		EffectiveMode: security.EffectiveApprovalMode(global, project.ApprovalMode),
	}
}
