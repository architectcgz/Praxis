// Package approval 负责审批模式配置与继承关系的查询和变更。
package approval

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/clock"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/core/security"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
)

// Dependencies 包含审批模式配置所需的持久化能力和全局默认值来源。
type Dependencies struct {
	ConfigMu           *sync.RWMutex
	Projects           repository.Project
	Clock              clock.Clock
	GlobalApprovalMode func() security.ApprovalMode
}

// Service 负责审批模式配置，与新 ToolCall 的规则读取共用配置锁。
type Service struct {
	configMu           *sync.RWMutex
	projects           repository.Project
	clock              clock.Clock
	globalApprovalMode func() security.ApprovalMode
}

// NewService 创建审批模式业务服务。
func NewService(deps Dependencies) (*Service, error) {
	if deps.ConfigMu == nil {
		deps.ConfigMu = &sync.RWMutex{}
	}
	if deps.GlobalApprovalMode == nil {
		return nil, errors.New("approval service global approval mode is required")
	}
	for name, value := range map[string]any{
		"projects": deps.Projects,
	} {
		if value == nil {
			return nil, fmt.Errorf("approval service %s is required", name)
		}
	}
	return &Service{
		configMu:           deps.ConfigMu,
		projects:           deps.Projects,
		clock:              clock.ClockOrDefault(deps.Clock),
		globalApprovalMode: deps.GlobalApprovalMode,
	}, nil
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
	s.configMu.Lock()
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
	s.configMu.Unlock()
	if err != nil {
		return ApprovalSettings{}, err
	}
	return s.approvalSettings(updated), nil
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
