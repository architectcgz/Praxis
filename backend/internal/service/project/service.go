// Package project 负责项目创建和配置查询。
package project

import (
	"context"
	"errors"
	projectmodel "praxis/internal/core/project"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"sync"
	"time"
)

// Directory 提供项目创建所需的目录操作能力。
type Directory interface {
	Ensure(context.Context, string) (created bool, err error)
}

const ErrProjectInvalid errorcode.Code = "project.invalid"

// Dependencies 包含项目创建和配置查询所需的依赖。
type Dependencies struct {
	Projects  repository.Project
	Directory Directory
}

// Service 管理项目记录。
type Service struct {
	createMu  sync.Mutex
	projects  repository.Project
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

// CreateProject 创建目录并保存项目配置；后续失败保留已创建目录供重试。
func (s *Service) CreateProject(ctx context.Context, params CreateInput) (projectmodel.Project, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	projects, err := s.projects.List(ctx, 0)
	if err != nil {
		return projectmodel.Project{}, err
	}
	for _, existing := range projects {
		if existing.ID != params.ProjectID && existing.Path == params.Path {
			return projectmodel.Project{}, errorcode.ErrRequestConflict
		}
	}
	if _, err := s.directory.Ensure(ctx, params.Path); err != nil {
		return projectmodel.Project{}, err
	}
	existing, err := s.projects.Get(ctx, params.ProjectID)
	if err == nil {
		if existing.Name != params.Name || existing.Path != params.Path {
			return projectmodel.Project{}, errorcode.ErrRequestConflict
		}
		return existing, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return projectmodel.Project{}, err
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
