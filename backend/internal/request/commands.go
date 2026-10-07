// Package request 定义进入应用服务前的 canonical request 和规范化入口。
//
// 该包不访问仓储、不持有锁，也不执行用例；成功返回的 request 才能交给 service。
package request

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// CreateProject 携带已规范化的项目创建参数。
type CreateProject struct {
	ProjectID   contracts.ProjectID
	WorkspaceID contracts.WorkspaceID
	Name        string
	Path        string
	RequestID   contracts.RequestID
}

// NewCreateProject 规范化项目名称和绝对路径；路径校验仍不访问文件系统。
func NewCreateProject(value CreateProject) (CreateProject, error) {
	value.Name = strings.TrimSpace(value.Name)
	value.Path = filepath.Clean(strings.TrimSpace(value.Path))
	if value.Name == "" || strings.ContainsAny(value.Name, "\\/:*?\"<>|\x00\r\n") ||
		value.Name == "." || value.Name == ".." || !filepath.IsAbs(value.Path) {
		return CreateProject{}, contracts.New(contracts.ProjectWorkspaceInvalid, "")
	}
	return value, nil
}

// CreateSession 携带已通过 wire shape 校验的会话创建参数。
type CreateSession struct {
	SessionID    contracts.SessionID
	AgentID      contracts.AgentID
	RequestID    contracts.RequestID
	ProjectID    contracts.ProjectID
	WorkspaceID  contracts.WorkspaceID
	DefinitionID contracts.AgentDefinitionID
}

// SendInput 携带已规范化的用户输入和模型选择。
type SendInput struct {
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	RequestID      contracts.RequestID
	Content        string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// NewSendInput 统一清洗并校验直接输入；大小按规范化后的 UTF-8 字节数计算。
func NewSendInput(value SendInput) (SendInput, error) {
	value.Content = strings.TrimSpace(value.Content)
	value.ProviderID = strings.TrimSpace(value.ProviderID)
	value.ModelID = strings.TrimSpace(value.ModelID)
	value.ReasoningLevel = strings.TrimSpace(value.ReasoningLevel)
	if value.ProviderID == "" || value.ModelID == "" {
		return SendInput{}, contracts.New(contracts.ModelNotConfigured, "")
	}
	if value.Content == "" || len(value.Content) > taskmodel.MaxInputBytes || !utf8.ValidString(value.Content) {
		return SendInput{}, contracts.InvalidValue("content", "输入必须是非空 UTF-8 文本且不超过大小限制")
	}
	return value, nil
}

// Control 固定控制目标，避免延迟请求影响后续任务。
type Control struct {
	CommandID    contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTaskID contracts.TaskID
}

// EnqueueTask 携带已规范化的预约任务参数。
type EnqueueTask struct {
	ID             contracts.TaskID
	RequestID      contracts.RequestID
	AgentID        contracts.AgentID
	Prompt         string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// NewEnqueueTask 统一清洗并校验预约输入。
func NewEnqueueTask(value EnqueueTask) (EnqueueTask, error) {
	value.Prompt = strings.TrimSpace(value.Prompt)
	value.ProviderID = strings.TrimSpace(value.ProviderID)
	value.ModelID = strings.TrimSpace(value.ModelID)
	value.ReasoningLevel = strings.TrimSpace(value.ReasoningLevel)
	if value.ProviderID == "" || value.ModelID == "" {
		return EnqueueTask{}, contracts.New(contracts.ModelNotConfigured, "")
	}
	if value.Prompt == "" || len(value.Prompt) > taskmodel.MaxInputBytes || !utf8.ValidString(value.Prompt) {
		return EnqueueTask{}, contracts.InvalidValue("prompt", "输入必须是非空 UTF-8 文本且不超过大小限制")
	}
	return value, nil
}

// RenameSession 携带已规范化的会话标题。
type RenameSession struct {
	SessionID contracts.SessionID
	Title     string
}

// NewRenameSession 清理并校验会话标题。
func NewRenameSession(sessionID contracts.SessionID, title string) (RenameSession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return RenameSession{}, contracts.InvalidValue("title", "session title is required")
	}
	return RenameSession{SessionID: sessionID, Title: title}, nil
}

// FilePreview 携带已规范化但尚未执行工作区授权的文件路径。
type FilePreview struct {
	SessionID contracts.SessionID
	Path      string
}

// NewFilePreview 处理平台路径表示并拒绝明显无效的路径。
func NewFilePreview(sessionID contracts.SessionID, path string) (FilePreview, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, '\x00') || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return FilePreview{}, contracts.InvalidValue("path", "文件路径无效。")
	}
	// Git Bash 会把 Windows 盘符路径表示为 /c/...。
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == '/' &&
		((path[1] >= 'a' && path[1] <= 'z') || (path[1] >= 'A' && path[1] <= 'Z')) {
		path = path[1:2] + ":" + path[2:]
	}
	return FilePreview{
		SessionID: sessionID,
		Path:      filepath.Clean(filepath.FromSlash(path)),
	}, nil
}
