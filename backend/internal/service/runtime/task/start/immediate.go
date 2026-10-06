package start

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
)

// SendInputParams 是直接输入的身份、正文和模型选择，不接受调用方预构建的 Context。
type SendInputParams struct {
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	RequestID      contracts.RequestID
	Content        string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

type Result struct {
	Task            taskmodel.Task
	ExistingRequest bool
}

// SendInput 在 Agent 没有活动 Task 时基于当前历史立即创建新的 Task。
func (s *Service) SendInput(ctx context.Context, params SendInputParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("send input context is required")
	}
	params.Content = strings.TrimSpace(params.Content)
	params.ProviderID = strings.TrimSpace(params.ProviderID)
	params.ModelID = strings.TrimSpace(params.ModelID)
	params.ReasoningLevel = strings.TrimSpace(params.ReasoningLevel)
	if params.Content == "" || len(params.Content) > taskmodel.MaxInputBytes || !utf8.ValidString(params.Content) {
		return Result{}, contracts.InvalidValue("content", "输入必须是非空 UTF-8 文本且不超过大小限制")
	}
	if params.AgentID == "" {
		agent, err := s.primary.GetOrCreatePrimaryAgent(ctx, params.SessionID, params.RequestID)
		if err != nil {
			return Result{}, err
		}
		params.AgentID = agent.ID
	}
	return s.createImmediate(ctx, params)
}

// createImmediate 在一次事务内完成新 Task 的准入与执行绑定。
func (s *Service) createImmediate(ctx context.Context, params SendInputParams) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("task creation context is required")
	}
	var result Result
	var admittedAgent contracts.AgentID
	var admittedTask contracts.TaskID
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.tasks.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if existing.Sequence != 0 ||
				existing.ProviderID != params.ProviderID || existing.ModelID != params.ModelID || existing.ReasoningLevel != params.ReasoningLevel ||
				params.SessionID != "" && existing.SessionID != params.SessionID {
				return contracts.ErrRequestConflict
			}
			agent, err := s.agents.Get(txCtx, existing.AgentID)
			if err != nil {
				return err
			}
			storedContent, err := s.inputMessageContent(txCtx, agent, existing.RequestID)
			if err != nil {
				return err
			}
			if storedContent != params.Content {
				return contracts.ErrRequestConflict
			}
			result = Result{Task: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if params.SessionID != "" && agent.SessionID != params.SessionID {
			return contracts.New(contracts.InvalidRequest, "")
		}
		if !agent.State.Startable() {
			return contracts.New(contracts.AgentExecuting, "")
		}
		active, err := s.tasks.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active != 0 {
			return contracts.New(contracts.AgentExecuting, "")
		}
		inputMessageID := "input:" + params.RequestID.String()
		input, err := s.buildInputSnapshot(txCtx, agent, params.ProviderID, params.ModelID, params.ReasoningLevel, params.Content, inputMessageID)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		task, err := taskmodel.NewTask(contracts.TaskID(s.ids.New("task")), agent.SessionID, agent.ID, params.RequestID, input, at)
		if err != nil {
			return err
		}
		task.ProviderID = params.ProviderID
		task.ModelID = params.ModelID
		task.ReasoningLevel = params.ReasoningLevel
		if err := agent.Start(task.ID, at); err != nil {
			return err
		}
		// 执行必须脱离事务 Context，避免 loop 后续写入已提交的事务工作区。
		_, created, err := s.executions.Start(ctx, agent.ID, task.ID)
		if err != nil {
			return err
		}
		if created {
			admittedAgent = agent.ID
			admittedTask = task.ID
		}
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		value := sessionmodel.MessageData{
			ID:         inputMessageID,
			RequestID:  params.RequestID.String(),
			TaskID:     task.ID.String(),
			Role:       sessionmodel.RoleUser,
			AuthorKind: sessionmodel.AuthorUser,
			Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: params.Content}},
			CreatedAt:  at,
		}
		if agent.CanReadSessionContext() {
			_, err = s.sessionMessages.Append(txCtx, sessionmodel.SessionMessage{SessionID: agent.SessionID, Data: value})
		} else {
			_, err = s.agentMessages.Append(txCtx, agentmodel.AgentMessage{AgentID: agent.ID, Data: value})
		}
		if err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Task = task
		return nil
	})
	if err != nil && admittedAgent != "" {
		s.executions.End(admittedAgent, admittedTask)
	}
	return result, err
}

func (s *Service) inputMessageContent(ctx context.Context, agent agentmodel.Agent, requestID contracts.RequestID) (string, error) {
	messageID := "input:" + requestID.String()
	if agent.CanReadSessionContext() {
		messages, err := s.sessionMessages.List(ctx, agent.SessionID, 0, 0)
		if err != nil {
			return "", err
		}
		for _, message := range messages {
			if message.Data.ID == messageID && message.Data.Role == sessionmodel.RoleUser {
				return message.Data.TextContent(), nil
			}
		}
	} else {
		messages, err := s.agentMessages.List(ctx, agent.ID, 0, 0)
		if err != nil {
			return "", err
		}
		for _, message := range messages {
			if message.Data.ID == messageID && message.Data.Role == sessionmodel.RoleUser {
				return message.Data.TextContent(), nil
			}
		}
	}
	return "", contracts.ErrNotFound
}
