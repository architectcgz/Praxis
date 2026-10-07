package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	coremodel "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/request"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	taskqueue "praxis/internal/service/runtime/queue"
	taskstart "praxis/internal/service/runtime/task/start"
)

// ListProjects 返回桌面项目目录，不向 binding 暴露领域实体。
func (s *Services) ListProjects(ctx context.Context, limit int) ([]ProjectSummary, error) {
	projects, err := s.projects.ListProjects(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]ProjectSummary, 0, len(projects))
	for _, project := range projects {
		result = append(result, ProjectSummary{
			ID:                 project.ID,
			Name:               project.Name,
			DefaultWorkspaceID: project.DefaultWorkspaceID,
			Path:               project.Path,
			State:              string(project.State),
		})
	}
	return result, nil
}

// ListSessionsByProject 返回指定项目的会话目录。
func (s *Services) ListSessionsByProject(ctx context.Context, projectID contracts.ProjectID, limit int) ([]SessionSummary, error) {
	sessions, err := s.sessions.ListSessionsByProject(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, SessionSummary{
			ID:          session.ID,
			ProjectID:   session.ProjectID,
			WorkspaceID: session.WorkspaceID,
			Title:       session.Title,
			CreatedAt:   session.CreatedAt,
			UpdatedAt:   session.UpdatedAt,
		})
	}
	return result, nil
}

// GetSessionDetail 返回会话和 Agent 摘要。
func (s *Services) GetSessionDetail(ctx context.Context, sessionID contracts.SessionID, limit int) (SessionDetail, error) {
	view, err := s.sessions.GetSessionView(ctx, sessionID, limit)
	if err != nil {
		return SessionDetail{}, err
	}
	result := SessionDetail{
		Session: SessionSummary{
			ID:          view.Session.ID,
			ProjectID:   view.Session.ProjectID,
			WorkspaceID: view.Session.WorkspaceID,
			Title:       view.Session.Title,
			CreatedAt:   view.Session.CreatedAt,
			UpdatedAt:   view.Session.UpdatedAt,
		},
		Agents: make([]AgentDetail, 0, len(view.Agents)),
	}
	for _, agent := range view.Agents {
		result.Agents = append(result.Agents, AgentDetail{
			ID:                     agent.ID,
			Name:                   agentmodel.DisplayName(agent.DefinitionID),
			SessionID:              agent.SessionID,
			DefinitionID:           agent.DefinitionID,
			SecurityPolicyRevision: agent.SecurityPolicyRevision,
			Profile:                string(agent.Profile),
			State:                  string(agent.State),
			CurrentTaskID:          agent.CurrentTaskID,
			Tasks:                  []TaskInfo{},
		})
	}
	return result, nil
}

// GetSessionUsageSummary 返回同一份已持久化用量快照。
func (s *Services) GetSessionUsageSummary(ctx context.Context, sessionID contracts.SessionID) (SessionUsageSummary, error) {
	summary, err := s.sessions.GetSessionUsageSummary(ctx, sessionID)
	if err != nil {
		return SessionUsageSummary{}, err
	}
	return SessionUsageSummary{
		Records:              modelUsageRecords(summary.Records),
		InputTokens:          summary.InputTokens,
		CacheReadInputTokens: summary.CacheReadInputTokens,
		CacheReadRatio:       clonePointer(summary.CacheReadRatio),
		CacheReadComplete:    summary.CacheReadComplete,
	}, nil
}

// DeleteSession 删除指定会话和关联文档。
func (s *Services) DeleteSession(ctx context.Context, sessionID contracts.SessionID) error {
	return s.sessions.DeleteSession(ctx, sessionID)
}

// RenameSession 更新指定会话的标题。
func (s *Services) RenameSession(ctx context.Context, canonical request.RenameSession) error {
	return s.sessions.RenameSession(ctx, canonical)
}

// PreviewFile 返回当前会话工作区内的文本快照。
func (s *Services) PreviewFile(ctx context.Context, canonical request.FilePreview) (FilePreview, error) {
	preview, err := s.sessions.PreviewFile(ctx, canonical)
	if err != nil {
		return FilePreview{}, err
	}
	return FilePreview{Path: preview.Path, Content: preview.Content}, nil
}

// GetAgentDetail 返回 Agent、任务和控制命令的公开状态。
func (s *Services) GetAgentDetail(ctx context.Context, agentID contracts.AgentID, limit int) (AgentDetail, error) {
	view, err := s.agents.GetAgentView(ctx, agentID, limit)
	if err != nil {
		return AgentDetail{}, err
	}
	result := AgentDetail{
		ID:                     view.Agent.ID,
		Name:                   agentmodel.DisplayName(view.Agent.DefinitionID),
		SessionID:              view.Agent.SessionID,
		DefinitionID:           view.Agent.DefinitionID,
		SecurityPolicyRevision: view.Agent.SecurityPolicyRevision,
		Profile:                string(view.Agent.Profile),
		State:                  string(view.Agent.State),
		CurrentTaskID:          view.Agent.CurrentTaskID,
		TaskIDs:                make([]string, 0, len(view.Tasks)),
		Tasks:                  make([]TaskInfo, 0, len(view.Tasks)),
		ControlCommandIDs:      make([]string, 0, len(view.Controls)),
	}
	for _, task := range view.Tasks {
		result.TaskIDs = append(result.TaskIDs, task.ID.String())
		result.Tasks = append(result.Tasks, taskInfo(task))
	}
	for _, control := range view.Controls {
		result.ControlCommandIDs = append(result.ControlCommandIDs, control.ID.String())
	}
	return result, nil
}

// ListAgentMessages 返回可公开展示的 Agent 消息。
func (s *Services) ListAgentMessages(ctx context.Context, agentID contracts.AgentID, limit int) ([]AgentMessage, error) {
	messages, err := s.agents.ListAgentMessages(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]AgentMessage, 0, len(messages))
	for _, message := range messages {
		if value, visible := publicAgentMessage(message); visible {
			result = append(result, value)
		}
	}
	return result, nil
}

// ListAgentHistory 返回排序后的公开消息及失败、取消任务。
func (s *Services) ListAgentHistory(ctx context.Context, agentID contracts.AgentID, limit int) ([]AgentHistoryItem, error) {
	messages, err := s.ListAgentMessages(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	view, err := s.agents.GetAgentView(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]AgentHistoryItem, 0, len(messages)+len(view.Tasks))
	for _, message := range messages {
		result = append(result, AgentHistoryItem{
			Kind:     "message",
			Sequence: message.Sequence,
			At:       message.At,
			Message:  &message,
		})
	}
	for _, task := range view.Tasks {
		if task.Status != taskmodel.TaskEnded {
			continue
		}
		kind := "task"
		if task.FailureCode == contracts.TaskFailureRequestCanceled {
			kind = "request_canceled"
		} else if task.Outcome != taskmodel.TaskFailed {
			continue
		}
		at := task.EndedAt
		if at.IsZero() {
			at = task.CreatedAt
		}
		value := taskInfo(task)
		result = append(result, AgentHistoryItem{Kind: kind, At: at, Task: &value})
	}
	slices.SortStableFunc(result, func(a, b AgentHistoryItem) int {
		if order := a.At.Compare(b.At); order != 0 {
			return order
		}
		return cmp.Compare(a.Sequence, b.Sequence)
	})
	return result, nil
}

func publicAgentMessage(value sessionmodel.MessageData) (AgentMessage, bool) {
	var content strings.Builder
	var thinking strings.Builder
	blocks := make([]MessageBlock, 0, len(value.Blocks))
	for _, block := range value.Blocks {
		if block.Kind == sessionmodel.BlockText {
			content.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockThinking {
			thinking.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockText || block.Kind == sessionmodel.BlockThinking ||
			block.Kind == sessionmodel.BlockToolCall || block.Kind == sessionmodel.BlockToolResult {
			blocks = append(blocks, MessageBlock{
				Kind:    string(block.Kind),
				Text:    block.Text,
				CallID:  block.CallID,
				Name:    block.Name,
				Input:   bytes.Clone(block.Input),
				IsError: block.IsError,
			})
		}
	}
	if (value.Role != sessionmodel.RoleUser && value.Role != sessionmodel.RoleAssistant && value.Role != sessionmodel.RoleTool) ||
		(strings.TrimSpace(content.String()) == "" && len(blocks) == 0 && strings.TrimSpace(thinking.String()) == "") {
		return AgentMessage{}, false
	}
	return AgentMessage{
		ID:         value.ID,
		Sequence:   value.Sequence,
		At:         value.CreatedAt,
		TaskID:     value.TaskID,
		Role:       string(value.Role),
		AuthorKind: string(value.AuthorKind),
		AuthorID:   value.AuthorID,
		Content:    content.String(),
		Thinking:   thinking.String(),
		Blocks:     blocks,
	}, true
}

func taskInfo(task taskmodel.Task) TaskInfo {
	return TaskInfo{
		ID:             task.ID,
		Status:         string(task.Status),
		Outcome:        string(task.Outcome),
		FailureCode:    string(task.FailureCode),
		FailureMessage: task.FailureMessage,
		CreatedAt:      task.CreatedAt,
		StartedAt:      task.StartedAt,
		EndedAt:        task.EndedAt,
	}
}

// CreateProject 组合项目用例并返回公开结果；目录操作由项目服务的 Port 处理。
func (s *Services) CreateProject(ctx context.Context, canonical request.CreateProject) (result CreateProjectResult, err error) {
	createdProject, err := s.projects.CreateProject(ctx, applicationproject.CreateProjectParams{
		RequestID:   canonical.RequestID,
		ProjectID:   canonical.ProjectID,
		WorkspaceID: canonical.WorkspaceID,
		Name:        canonical.Name,
		Path:        canonical.Path,
	})
	if err != nil {
		return result, err
	}
	return CreateProjectResult{
		ProjectID:   createdProject.Project.ID,
		Name:        createdProject.Project.Name,
		WorkspaceID: createdProject.Workspace.ID,
		Path:        createdProject.Workspace.Path,
	}, nil
}

// CreateSession 创建会话和主 Agent。
func (s *Services) CreateSession(ctx context.Context, canonical request.CreateSession) (CreateSessionResult, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	created, err := s.sessions.CreateSessionForProject(
		ctx,
		canonical.SessionID,
		canonical.AgentID,
		canonical.RequestID,
		canonical.ProjectID,
		canonical.WorkspaceID,
		canonical.DefinitionID,
	)
	if err != nil {
		return CreateSessionResult{}, err
	}
	return CreateSessionResult{
		SessionID:    created.Session.ID,
		ProjectID:    created.Session.ProjectID,
		WorkspaceID:  created.Session.WorkspaceID,
		AgentID:      created.Agent.ID,
		DefinitionID: created.Agent.DefinitionID,
	}, nil
}

// SendInput 提交用户输入并返回持久化任务身份。
func (s *Services) SendInput(ctx context.Context, canonical request.SendInput) (SendInputResult, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	params := taskstart.SendInputParams{
		SessionID:      canonical.SessionID,
		AgentID:        canonical.AgentID,
		RequestID:      canonical.RequestID,
		Content:        canonical.Content,
		ProviderID:     canonical.ProviderID,
		ModelID:        canonical.ModelID,
		ReasoningLevel: canonical.ReasoningLevel,
	}
	started, err := s.runtime.SendInput(ctx, params)
	if err != nil {
		return SendInputResult{}, err
	}
	return SendInputResult{
		TaskID:          started.Task.ID,
		ExistingRequest: started.ExistingRequest,
		ActivationError: started.ActivationError,
	}, nil
}

// PauseAgent 持久化暂停命令后通知当前执行。
func (s *Services) PauseAgent(ctx context.Context, canonical request.Control) (ControlResult, error) {
	result, err := s.agents.PauseAgent(ctx, applicationagent.ControlParams{
		CommandID:    canonical.CommandID,
		AgentID:      canonical.AgentID,
		TargetTaskID: canonical.TargetTaskID,
	})
	if err != nil {
		return ControlResult{}, err
	}
	return controlResult(result), nil
}

// CancelTask 持久化取消命令后通知当前执行。
func (s *Services) CancelTask(ctx context.Context, canonical request.Control) (ControlResult, error) {
	result, err := s.agents.StopAgent(ctx, applicationagent.ControlParams{
		CommandID:    canonical.CommandID,
		AgentID:      canonical.AgentID,
		TargetTaskID: canonical.TargetTaskID,
	})
	if err != nil {
		return ControlResult{}, err
	}
	return controlResult(result), nil
}

func controlResult(result applicationagent.ControlResult) ControlResult {
	return ControlResult{
		Command: ControlInfo{
			ID:           result.Command.ID,
			AgentID:      result.Command.AgentID,
			TargetTaskID: result.Command.TargetTaskID,
			Kind:         string(result.Command.Kind),
			Status:       string(result.Command.Status),
		},
		ExistingCommand:   result.ExistingCommand,
		CancellationError: result.CancellationError,
	}
}

// EnqueueTask 保存预约任务，不触发当前 runtime。
func (s *Services) EnqueueTask(ctx context.Context, canonical request.EnqueueTask) (EnqueueTaskResult, error) {
	result, err := s.runtime.EnqueueTask(ctx, taskqueue.EnqueueParams{
		ID:             canonical.ID,
		RequestID:      canonical.RequestID,
		AgentID:        canonical.AgentID,
		Prompt:         canonical.Prompt,
		ProviderID:     canonical.ProviderID,
		ModelID:        canonical.ModelID,
		ReasoningLevel: canonical.ReasoningLevel,
	})
	if err != nil {
		return EnqueueTaskResult{}, err
	}
	return EnqueueTaskResult{
		TaskID:       result.Task.ID,
		Status:       string(result.Task.Status),
		ExistingTask: result.ExistingTask,
	}, nil
}

// ListModels 返回已确认的模型能力和引用该模型的 Agent。
func (s *Services) ListModels() []ModelOption {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.models == nil {
		return []ModelOption{}
	}
	models := s.models.ListModels()
	result := make([]ModelOption, 0, len(models))
	for _, option := range models {
		assigned := slices.Clone(option.AssignedAgentDefinitions)
		if s.agentConfig != nil {
			assigned = s.agentConfig.DefinitionsForModel(option.ProviderID, option.ModelID)
		}
		result = append(result, ModelOption{
			ProviderID:               option.ProviderID,
			ModelID:                  option.ModelID,
			DefaultProviderID:        option.DefaultProviderID,
			DefaultModelID:           option.DefaultModelID,
			Label:                    option.Label,
			ProviderName:             option.ProviderName,
			ReasoningLevels:          slices.Clone(option.ReasoningLevels),
			DefaultReasoningLevel:    option.DefaultReasoningLevel,
			AssignedAgentDefinitions: assigned,
		})
	}
	return result
}

// ListAgent 返回 Agent 最近的独立计时记录。
func (s *Services) ListAgent(ctx context.Context, agentID string, limit int) ([]OperationTiming, error) {
	if s.listAgentTimings == nil {
		return nil, errors.New("timing query is unavailable")
	}
	records, err := s.listAgentTimings(ctx, agentID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]OperationTiming, 0, len(records))
	for _, record := range records {
		result = append(result, OperationTiming{
			SessionID:       record.SessionID,
			AgentID:         record.AgentID,
			TaskID:          record.TaskID,
			Kind:            string(record.Kind),
			Name:            record.Name,
			ReferenceID:     record.ReferenceID,
			ID:              record.ID,
			ParentID:        record.ParentID,
			StartedAt:       record.StartedAt,
			FinishedAt:      record.FinishedAt,
			DurationMS:      clonePointer(record.DurationMS),
			FirstResponseMS: clonePointer(record.FirstResponseMS),
			Status:          string(record.Status),
			Revision:        record.Revision,
		})
	}
	return result, nil
}

// ListSession 返回会话所有请求的已上报用量。
func (s *Services) ListSession(ctx context.Context, sessionID string) ([]ModelUsageRecord, error) {
	if s.listSessionUsage == nil {
		return nil, errors.New("usage query is unavailable")
	}
	records, err := s.listSessionUsage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return modelUsageRecords(records), nil
}

func modelUsageRecords(records []coremodel.ModelUsageRecord) []ModelUsageRecord {
	if records == nil {
		return nil
	}
	result := make([]ModelUsageRecord, 0, len(records))
	for _, record := range records {
		result = append(result, ModelUsageRecord{
			SessionID: record.SessionID,
			AgentID:   record.AgentID,
			TaskID:    record.TaskID,
			TurnID:    record.TurnID,
			Usage: ModelUsage{
				InputTokens:              record.Usage.InputTokens,
				OutputTokens:             clonePointer(record.Usage.OutputTokens),
				CacheReadInputTokens:     clonePointer(record.Usage.CacheReadInputTokens),
				CacheCreationInputTokens: clonePointer(record.Usage.CacheCreationInputTokens),
			},
		})
	}
	return result
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
