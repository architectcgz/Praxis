package project

import (
	"fmt"
	"strings"
	"time"

	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/utils/pathutil"
)

// ProjectState 表示用户项目的生命周期状态。
type ProjectState string

const (
	ProjectActive   ProjectState = "active"
	ProjectArchived ProjectState = "archived"
)

// Project 保存用户项目的持久化身份和目录记录。
// Session 通过 ProjectID 关联，工具执行与文件访问使用项目目录。
type Project struct {
	ID        identity.ProjectID
	Name      string
	Path      string
	State     ProjectState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewProject 使用入口已校验的身份、名称和路径创建项目，初始状态为 active。
func NewProject(
	id identity.ProjectID,
	name string,
	path string,
	at time.Time,
) Project {
	return Project{
		ID:        id,
		Name:      name,
		Path:      path,
		State:     ProjectActive,
		CreatedAt: at.UTC(),
		UpdatedAt: at.UTC(),
	}
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (p Project) Validate() error {
	if !p.ID.Valid() {
		return fmt.Errorf("project.id: invalid project identity: %w", errorcode.ErrInvalidValue)
	}
	if p.Name == "" || strings.ContainsAny(p.Name, "\x00\r\n") {
		return fmt.Errorf("project.name: name is required and cannot contain control characters: %w", errorcode.ErrInvalidValue)
	}
	if !pathutil.IsAbsoluteNormalized(p.Path) {
		return fmt.Errorf("project.path: path must be an absolute normalized path: %w", errorcode.ErrInvalidValue)
	}
	if p.State != ProjectActive && p.State != ProjectArchived {
		return fmt.Errorf("project.state: unknown project state: %w", errorcode.ErrInvalidValue)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return fmt.Errorf("project.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}
