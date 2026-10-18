// Package request 定义进入应用服务前的 canonical request 和规范化入口。
//
// 该包不访问仓储、不持有锁，也不执行用例；成功返回的 request 才能交给 service。
package request

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"praxis/internal/core/fault"
	modelconfig "praxis/internal/core/model/config"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
)

// CreateProject 携带已规范化的项目创建参数。
type CreateProject struct {
	ProjectID   string
	WorkspaceID string
	Name        string
	Path        string
	RequestID   string
}

// NewCreateProject 规范化项目名称和绝对路径；路径校验仍不访问文件系统。
func NewCreateProject(value CreateProject) (CreateProject, error) {
	value.Name = strings.TrimSpace(value.Name)
	value.Path = filepath.Clean(strings.TrimSpace(value.Path))
	if value.Name == "" || strings.ContainsAny(value.Name, "\\/:*?\"<>|\x00\r\n") ||
		value.Name == "." || value.Name == ".." || !filepath.IsAbs(value.Path) || strings.ContainsAny(value.Path, "\x00\r\n") {
		return CreateProject{}, fault.New(projectmodel.ProjectWorkspaceInvalid, "")
	}
	return value, nil
}

// CreateSession 携带已通过 wire shape 校验的会话创建参数。
type CreateSession struct {
	SessionID    string
	AgentID      string
	RequestID    string
	ProjectID    string
	WorkspaceID  string
	DefinitionID string
}

// SendInput 携带已规范化的用户输入和模型选择。
type SendInput struct {
	SessionID      string
	AgentID        string
	RequestID      string
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
		return SendInput{}, fault.New(modelconfig.ModelNotConfigured, "")
	}
	if value.Content == "" || len(value.Content) > taskmodel.MaxInputBytes || !utf8.ValidString(value.Content) {
		return SendInput{}, fault.InvalidValue("content", "content must be non-empty UTF-8 text within the size limit")
	}
	return value, nil
}

// Control 固定控制目标，避免延迟请求影响后续任务。
type Control struct {
	CommandID    string
	AgentID      string
	TargetTaskID string
}

// EnqueueTask 携带已规范化的预约任务参数。
type EnqueueTask struct {
	ID             string
	RequestID      string
	AgentID        string
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
		return EnqueueTask{}, fault.New(modelconfig.ModelNotConfigured, "")
	}
	if value.Prompt == "" || len(value.Prompt) > taskmodel.MaxInputBytes || !utf8.ValidString(value.Prompt) {
		return EnqueueTask{}, fault.InvalidValue("prompt", "prompt must be non-empty UTF-8 text within the size limit")
	}
	return value, nil
}

// RenameSession 携带已规范化的会话标题。
type RenameSession struct {
	SessionID string
	Title     string
}

// NewRenameSession 清理并校验会话标题。
func NewRenameSession(sessionID string, title string) (RenameSession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return RenameSession{}, fault.InvalidValue("title", "session title is required")
	}
	return RenameSession{SessionID: sessionID, Title: title}, nil
}

// FilePreview 携带已规范化但尚未执行工作区授权的文件路径。
type FilePreview struct {
	SessionID string
	Path      string
}

// NewFilePreview 处理平台路径表示并拒绝明显无效的路径。
func NewFilePreview(sessionID string, path string) (FilePreview, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, '\x00') || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return FilePreview{}, fault.InvalidValue("path", "file path is invalid")
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

// AppendSessionContext 携带已规范化的共享事实输入，SourceTaskID 可以为空。
type AppendSessionContext struct {
	EntryID      string
	SessionID    string
	Kind         sessionmodel.SessionContextKind
	SourceTaskID string
	Content      string
}

// NewAppendSessionContext 一次规范化并校验共享事实，拒绝空正文、非法 UTF-8 和超限内容。
func NewAppendSessionContext(value AppendSessionContext) (AppendSessionContext, error) {
	value.EntryID = strings.TrimSpace(value.EntryID)
	value.SessionID = strings.TrimSpace(value.SessionID)
	value.SourceTaskID = strings.TrimSpace(value.SourceTaskID)
	value.Content = strings.TrimSpace(value.Content)
	if value.EntryID == "" || value.SessionID == "" {
		return AppendSessionContext{}, fault.InvalidValue("sessionContext", "entry id and session id are required")
	}
	for _, id := range []string{value.EntryID, value.SessionID, value.SourceTaskID} {
		if strings.ContainsAny(id, "\x00\r\n") {
			return AppendSessionContext{}, fault.InvalidValue("sessionContext", "identity contains control characters")
		}
	}
	switch value.Kind {
	case sessionmodel.SessionContextUserMessage, sessionmodel.SessionContextAcceptedConclusion, sessionmodel.SessionContextDecision, sessionmodel.SessionContextReference:
	default:
		return AppendSessionContext{}, fault.InvalidValue("sessionContext.kind", "unknown context entry kind")
	}
	if value.Content == "" || !utf8.ValidString(value.Content) || len(value.Content) > sessionmodel.MaxSessionContextContentBytes {
		return AppendSessionContext{}, fault.InvalidValue("sessionContext.content", "content must be non-empty UTF-8 text within the size limit")
	}
	return value, nil
}
