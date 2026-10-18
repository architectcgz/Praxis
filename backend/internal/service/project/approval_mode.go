package project

import (
	"context"
	"praxis/internal/identity"
	"praxis/internal/security"
)

// ApprovalSettings 返回业务层解析的继承关系和有效模式，与工具启动使用相同规则。
type ApprovalSettings struct {
	ProjectID     identity.ProjectID
	GlobalMode    security.ApprovalMode
	ProjectMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

// GetApprovalMode 在与工具启动相同的事务边界读取项目与全局模式。
func (s *Service) GetApprovalMode(ctx context.Context, projectID identity.ProjectID) (ApprovalSettings, error) {
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
func (s *Service) SetApprovalMode(ctx context.Context, projectID identity.ProjectID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	var result ApprovalSettings
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		project, err := s.projects.Get(txCtx, projectID)
		if err != nil {
			return err
		}
		if project.ApprovalMode == nil && mode == nil || project.ApprovalMode != nil && mode != nil && *project.ApprovalMode == *mode {
			result = s.approvalSettings(project)
			return nil
		}
		project.ApprovalMode = mode
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

func (s *Service) approvalSettings(project Project) ApprovalSettings {
	global := s.globalApprovalMode()
	return ApprovalSettings{
		ProjectID:     project.ID,
		GlobalMode:    global,
		ProjectMode:   project.ApprovalMode,
		EffectiveMode: security.EffectiveApprovalMode(global, project.ApprovalMode),
	}
}
