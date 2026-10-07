package service

import (
	"encoding/json"
	"time"

	"praxis/internal/contracts"
)

// ProjectSummary 是项目目录中的稳定展示数据。
type ProjectSummary struct {
	ID                 contracts.ProjectID
	Name               string
	DefaultWorkspaceID contracts.WorkspaceID
	Path               string
	State              string
}

// CreateProjectResult 返回项目创建后的公开数据。
type CreateProjectResult struct {
	ProjectID   contracts.ProjectID
	Name        string
	WorkspaceID contracts.WorkspaceID
	Path        string
}

// SessionSummary 是会话目录中的稳定展示数据。
type SessionSummary struct {
	ID          contracts.SessionID
	ProjectID   contracts.ProjectID
	WorkspaceID contracts.WorkspaceID
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SessionDetail 返回会话及其 Agent 摘要。
type SessionDetail struct {
	Session SessionSummary
	Agents  []AgentDetail
}

// CreateSessionResult 返回已创建会话的关联身份。
type CreateSessionResult struct {
	SessionID    contracts.SessionID
	ProjectID    contracts.ProjectID
	WorkspaceID  contracts.WorkspaceID
	AgentID      contracts.AgentID
	DefinitionID contracts.AgentDefinitionID
}

// AgentDetail 仅保留桌面端需要的 Agent 状态。
type AgentDetail struct {
	ID                     contracts.AgentID
	Name                   string
	SessionID              contracts.SessionID
	DefinitionID           contracts.AgentDefinitionID
	SecurityPolicyRevision uint64
	Profile                string
	State                  string
	CurrentTaskID          contracts.TaskID
	TaskIDs                []string
	Tasks                  []TaskInfo
	ControlCommandIDs      []string
}

// TaskInfo 是已持久化任务的安全展示数据。
type TaskInfo struct {
	ID             contracts.TaskID
	Status         string
	Outcome        string
	FailureCode    string
	FailureMessage string
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
}

// ControlInfo 返回控制命令的持久化状态。
type ControlInfo struct {
	ID           contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTaskID contracts.TaskID
	Kind         string
	Status       string
}

// AgentMessage 返回过滤后的公开消息及结构化内容。
type AgentMessage struct {
	ID         string
	Sequence   uint64
	At         time.Time
	TaskID     string
	Role       string
	AuthorKind string
	AuthorID   string
	Content    string
	Thinking   string
	Blocks     []MessageBlock
}

// MessageBlock 仅包含桌面端可展示的内容块。
type MessageBlock struct {
	Kind    string
	Text    string
	CallID  string
	Name    string
	Input   json.RawMessage
	IsError bool
}

// AgentHistoryItem 合并消息及需要展示的任务终态。
type AgentHistoryItem struct {
	Kind     string
	Sequence uint64
	At       time.Time
	Message  *AgentMessage
	Task     *TaskInfo
}

// SendInputResult 区分持久化结果与激活失败。
type SendInputResult struct {
	TaskID          contracts.TaskID
	ExistingRequest bool
	ActivationError string
}

// ControlResult 返回控制命令及幂等状态。
type ControlResult struct {
	Command           ControlInfo
	ExistingCommand   bool
	CancellationError string
}

// EnqueueTaskResult 返回队列身份与状态。
type EnqueueTaskResult struct {
	TaskID       contracts.TaskID
	Status       string
	ExistingTask bool
}

// ModelOption 返回可选模型及关联的 Agent 定义。
type ModelOption struct {
	ProviderID               string
	ModelID                  string
	DefaultProviderID        string
	DefaultModelID           string
	Label                    string
	ProviderName             string
	ReasoningLevels          []string
	DefaultReasoningLevel    string
	AssignedAgentDefinitions []string
}

// FilePreview 是授权工作区内的文本快照。
type FilePreview struct {
	Path    string
	Content string
}

// ModelUsage 保留未知 token 计数与已知零值的区别。
type ModelUsage struct {
	InputTokens              int64
	OutputTokens             *int64
	CacheReadInputTokens     *int64
	CacheCreationInputTokens *int64
}

// ModelUsageRecord 是一次请求的持久化用量数据。
type ModelUsageRecord struct {
	SessionID string
	AgentID   string
	TaskID    string
	TurnID    contracts.TurnID
	Usage     ModelUsage
}

// SessionUsageSummary 保留同一快照的记录与可选缓存比例。
type SessionUsageSummary struct {
	Records              []ModelUsageRecord
	InputTokens          int64
	CacheReadInputTokens int64
	CacheReadRatio       *float64
	CacheReadComplete    bool
}

// OperationTiming 是操作计时数据，未知耗时不能补零。
type OperationTiming struct {
	SessionID       string
	AgentID         string
	TaskID          string
	Kind            string
	Name            string
	ReferenceID     string
	ID              string
	ParentID        string
	StartedAt       time.Time
	FinishedAt      time.Time
	DurationMS      *int64
	FirstResponseMS *int64
	Status          string
	Revision        int
}

// AgentEvent 是 application 拥有的瞬时通知，终态后仍应查询持久化事实。
type AgentEvent struct {
	Kind           string
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	TaskID         contracts.TaskID
	Outcome        string
	FailureCode    string
	FailureMessage string
	TurnID         contracts.TurnID
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage
	Result         string
	IsError        bool
	Error          string
	Timing         *OperationTiming
	Usage          *ModelUsage
}

// ModelConfigGroup 是设置页可编辑的模型分组。
type ModelConfigGroup struct {
	ID          string
	DisplayName string
}

// ModelConfigProvider 是设置页可编辑的 Provider 元数据，不包含凭据。
type ModelConfigProvider struct {
	ID             string
	DisplayName    string
	BaseURL        string
	ProxyURL       string
	DefaultModelID string
	HasAPIKey      bool
}

// ConfiguredModel 是设置页可编辑的模型能力。
type ConfiguredModel struct {
	ProviderID            string
	ModelID               string
	DisplayName           string
	GroupID               string
	APIFormat             string
	ContextWindow         int
	MaxOutputTokens       int
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

// ModelConfig 是完整的可编辑模型配置，不包含凭据内容。
type ModelConfig struct {
	Groups            []ModelConfigGroup
	DefaultProviderID string
	Providers         []ModelConfigProvider
	Models            []ConfiguredModel
}
