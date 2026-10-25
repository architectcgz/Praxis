// Package approval 负责审批模式配置与继承关系的查询和变更。
package approval

import (
	"context"
	"praxis/internal/clock"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

// Dependencies 包含项目审批配置的持久化能力和只读配置快照来源。
type Dependencies struct {
	Config   ConfigSource
	Projects repository.Project
	Clock    clock.Clock
}

// Service 负责项目审批模式配置；配置同步由快照来源管理。
type Service struct {
	config   ConfigSource
	projects repository.Project
	clock    clock.Clock
	// updateMu 串行化本服务对项目审批模式的读改写，不影响配置快照的读取。
	updateMu sync.Mutex
}

// NewService 创建审批模式业务服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		config:   deps.Config,
		projects: deps.Projects,
		clock:    deps.Clock,
	}
}

// ApprovalSettings 返回业务层解析的继承关系和有效模式，与工具启动使用相同规则。
type ApprovalSettings struct {
	ProjectID     identity.ProjectID
	GlobalMode    security.ApprovalMode
	ProjectMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

// GetApprovalMode 组合项目覆盖与当前全局模式，供配置界面展示。
func (s *Service) GetApprovalMode(ctx context.Context, projectID identity.ProjectID) (ApprovalSettings, error) {
	var result ApprovalSettings
	err := func(txCtx context.Context) error {
		project, err := s.projects.Get(txCtx, projectID)
		if err != nil {
			return err
		}
		result = s.approvalSettings(project)
		return nil
	}(ctx)
	return result, err
}

// SetApprovalMode 持久化项目覆盖并返回提交后的有效模式；写入失败不返回成功状态。
func (s *Service) SetApprovalMode(ctx context.Context, projectID identity.ProjectID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	var updated projectmodel.Project
	s.updateMu.Lock()
	err := func(txCtx context.Context) error {
		project, err := s.projects.Get(txCtx, projectID)
		if err != nil {
			return err
		}
		if project.ApprovalMode == nil && mode == nil || project.ApprovalMode != nil && mode != nil && *project.ApprovalMode == *mode {
			updated = project
			return nil
		}
		project.ApprovalMode = mode
		project.UpdatedAt = s.clock.Now().UTC()
		if err := s.projects.Save(txCtx, project); err != nil {
			return err
		}
		updated = project
		return nil
	}(ctx)
	s.updateMu.Unlock()
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(updated), nil
}

func (s *Service) approvalSettings(project projectmodel.Project) ApprovalSettings {
	global := s.config.ApprovalConfig().GlobalMode
	return ApprovalSettings{
		ProjectID:     project.ID,
		GlobalMode:    global,
		ProjectMode:   project.ApprovalMode,
		EffectiveMode: security.EffectiveApprovalMode(global, project.ApprovalMode),
	}
}
