// Package project 负责项目创建和配置查询。
package project

import (
	"context"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	projectmodel "praxis/internal/project"
	"sync"
	"time"
)

// Directory 提供项目创建所需的目录操作能力。
type Directory interface {
	EnsureExists(context.Context, string) (created bool, err error)
}

type ProjectStore interface {
	Save(context.Context, projectmodel.Project) error
	List(context.Context, int) ([]projectmodel.Project, error)
}

// Dependencies 包含项目创建和配置查询所需的依赖。
type Dependencies struct {
	Projects  ProjectStore
	Directory Directory
}

// Service 管理项目记录。
type Service struct {
	createMu  sync.Mutex
	projects  ProjectStore
	directory Directory
}

// CreateInput 包含输入边界已校验的身份及规范化后的项目名称、绝对路径。
type CreateInput struct {
	ProjectID identity.ProjectID
	Name      string
	Path      string
}

// NewService 创建项目业务服务。
func NewService(deps Dependencies) *Service {
	return &Service{
		projects:  deps.Projects,
		directory: deps.Directory,
	}
}

// CreateProject 先准备目录，再登记项目；后续冲突或保存失败不会清理已创建目录。
func (s *Service) CreateProject(ctx context.Context, params CreateInput) (projectmodel.Project, error) {
	if _, err := s.directory.EnsureExists(ctx, params.Path); err != nil {
		return projectmodel.Project{}, err
	}
	// 仓库按项目 ID 保存，路径查重和保存必须在同一临界区。
	s.createMu.Lock()
	defer s.createMu.Unlock()

	projects, err := s.projects.List(ctx, 0)
	if err != nil {
		return projectmodel.Project{}, err
	}
	var prior *projectmodel.Project
	for i := range projects {
		existing := &projects[i]
		if existing.ID == params.ProjectID {
			if existing.Name != params.Name || existing.Path != params.Path {
				return projectmodel.Project{}, errorcode.ErrRequestConflict
			}
			prior = existing
		} else if existing.Path == params.Path {
			return projectmodel.Project{}, errorcode.ErrRequestConflict
		}
	}
	if prior != nil {
		return *prior, nil
	}
	project := projectmodel.NewProject(params.ProjectID, params.Name, params.Path, time.Now().UTC())
	if err := s.projects.Save(ctx, project); err != nil {
		return projectmodel.Project{}, err
	}
	return project, nil
}

// ListProjects 返回按更新时间排序的项目列表。
func (s *Service) ListProjects(ctx context.Context, limit int) ([]projectmodel.Project, error) {
	return s.projects.List(ctx, limit)
}
