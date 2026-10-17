package task

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
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

// SendInputResult 返回持久化 Task 及请求是否已存在；重复请求不建立新的执行绑定。
type SendInputResult struct {
	Task            taskmodel.Task
	ExistingRequest bool
}

// SendInput 在 Agent 没有活动 Task 时基于当前历史立即创建新的 Task。
func (s *Service) SendInput(ctx context.Context, params SendInputParams) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("send input context is required")
	}
	if params.AgentID == "" {
		agent, err := s.primary.GetOrCreatePrimaryAgent(ctx, params.SessionID, params.RequestID)
		if err != nil {
			return SendInputResult{}, err
		}
		params.AgentID = agent.ID
	}
	return s.createImmediate(ctx, params)
}

// createImmediate 在一次事务内完成新 Task 的准入与执行绑定。
func (s *Service) createImmediate(ctx context.Context, params SendInputParams) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("task creation context is required")
	}
	var committedMessage *sessionmodel.MessageData
	var result SendInputResult
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
			result = SendInputResult{Task: existing, ExistingRequest: true}
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
		config, err := s.buildConfig(txCtx, agent, params.ProviderID, params.ModelID, params.ReasoningLevel)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		taskID := contracts.TaskID(s.ids.New("task"))
		value := sessionmodel.MessageData{
			ID:         inputMessageID,
			RequestID:  params.RequestID.String(),
			TaskID:     taskID.String(),
			Role:       sessionmodel.RoleUser,
			AuthorKind: sessionmodel.AuthorUser,
			Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: params.Content}},
			CreatedAt:  at,
		}
		stored, err := s.messages.Append(txCtx, agent, value)
		if err != nil {
			return err
		}
		committedMessage = &stored
		selection, err := s.selectContext(txCtx, agent, config, inputMessageID)
		if err != nil {
			return err
		}
		task := taskmodel.NewTask(taskID, agent.SessionID, agent.ID, params.RequestID, inputMessageID, config, selection, at)
		task.ProviderID = params.ProviderID
		task.ModelID = params.ModelID
		task.ReasoningLevel = params.ReasoningLevel
		if err := agent.Start(task.ID, at); err != nil {
			return err
		}
		// 执行必须脱离事务 Context，避免 loop 后续写入已提交的事务工作区。
		_, created, err := s.executions.Reserve(ctx, agent.ID, task.ID)
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
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Task = task
		return nil
	})
	if err != nil && admittedAgent != "" {
		s.executions.Release(admittedAgent, admittedTask)
	}
	if err == nil && committedMessage != nil && !result.ExistingRequest {
		if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hooks.CommittedEvent{
			Identity: hooks.Identity{
				SessionID: result.Task.SessionID,
				AgentID:   result.Task.AgentID,
				TaskID:    result.Task.ID,
			},
			Message: committedMessage,
		}); hookErr != nil {
			s.logger.Errorf("user message committed hook failed: %v", hookErr)
		}
	}
	return result, err
}

func (s *Service) inputMessageContent(ctx context.Context, agent agentmodel.Agent, requestID contracts.RequestID) (string, error) {
	messageID := "input:" + requestID.String()
	messages, err := s.messages.List(ctx, agent, 0, 0)
	if err != nil {
		return "", err
	}
	for _, message := range messages {
		if message.ID == messageID && message.Role == sessionmodel.RoleUser {
			return message.TextContent(), nil
		}
	}
	return "", contracts.ErrNotFound
}
