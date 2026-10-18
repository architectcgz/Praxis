// Package contracts 定义项目应用入口的公开数据，不暴露内核类型。
package contracts

// ProjectSummary 是项目目录中的稳定展示数据。
type ProjectSummary struct {
	ID                 string
	Name               string
	DefaultWorkspaceID string
	Path               string
	State              string
}

// CreateProjectResult 返回项目创建后的公开数据。
type CreateProjectResult struct {
	ProjectID   string
	Name        string
	WorkspaceID string
	Path        string
}
