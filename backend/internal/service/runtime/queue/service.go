// Package queue 负责 runtime 的持久化任务队列，保存待执行内容并按 FIFO 提供下一项。
package queue

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"praxis/internal/system"

	"context"
	"errors"
	"fmt"
)

type Config struct {
	HookDispatcher *hooks.Dispatcher
	Logger         *logging.Logger
	Transactions   repository.TxRunner
	Agents         repository.SessionAgentRepository
	Tasks          repository.TaskRepository
	Messages       repository.MessageStreams
	Clock          system.Clock
}

type Service struct {
	hookDispatcher *hooks.Dispatcher
	logger         *logging.Logger
	tx             repository.TxRunner
	agents         repository.SessionAgentRepository
	tasks          repository.TaskRepository
	messages       repository.MessageStreams
	clock          system.Clock
}

type EnqueueParams struct {
	ID             contracts.TaskID
	RequestID      contracts.RequestID
	AgentID        contracts.AgentID
	Prompt         string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

type EnqueueResult struct {
	Task         taskmodel.Task
	ExistingTask bool
}

func NewService(config Config) (*Service, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"agents":       config.Agents,
		"tasks":        config.Tasks,
		"messages":     config.Messages,
	} {
		if value == nil {
			return nil, fmt.Errorf("task queue service %s is required", name)
		}
	}
	return &Service{
		hookDispatcher: config.HookDispatcher,
		logger:         logging.NewFactory().Ensure(config.Logger),
		tx:             config.Transactions,
		agents:         config.Agents,
		tasks:          config.Tasks,
		messages:       config.Messages,
		clock:          system.ClockOrDefault(config.Clock),
	}, nil
}

// EnqueueTask 在 main agent 忙碌时预约 Task；同一请求返回原 Task，输入不同则拒绝。
func (s *Service) EnqueueTask(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue task context is required")
	}
	var committedMessage *sessionmodel.MessageData
	result := EnqueueResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if !agent.CanReadSessionContext() {
			return contracts.New(contracts.InvalidRequest, "queued input requires the session main agent")
		}
		existingRequest, err := s.tasks.FindByRequest(txCtx, agent.ID, params.RequestID)
		if err == nil {
			if existingRequest.ID != params.ID || existingRequest.Sequence == 0 ||
				existingRequest.ProviderID != params.ProviderID || existingRequest.ModelID != params.ModelID || existingRequest.ReasoningLevel != params.ReasoningLevel {
				return contracts.ErrRequestConflict
			}
			storedContent, err := s.inputMessageContent(txCtx, agent, existingRequest.RequestID)
			if err != nil {
				return err
			}
			if storedContent != params.Prompt {
				return contracts.ErrRequestConflict
			}
			result.Task, result.ExistingTask = existingRequest, true
			return nil
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		_, err = s.tasks.Get(txCtx, params.ID)
		if err == nil {
			return contracts.ErrRequestConflict
		}
		if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if agent.State != agentmodel.AgentExecuting || agent.CurrentTaskID == "" {
			return contracts.New(contracts.AgentUnavailable, "queue input requires an executing runtime")
		}
		sequence, err := s.tasks.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		task, err := taskmodel.NewPendingTask(
			params.ID, agent.SessionID, agent.ID, params.RequestID, "input:"+params.RequestID.String(), sequence, at,
		)
		if err != nil {
			return err
		}
		task.ProviderID = params.ProviderID
		task.ModelID = params.ModelID
		task.ReasoningLevel = params.ReasoningLevel
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		stored, err := s.messages.Append(txCtx, agent, sessionmodel.MessageData{
			ID:         "input:" + params.RequestID.String(),
			RequestID:  params.RequestID.String(),
			TaskID:     task.ID.String(),
			Role:       sessionmodel.RoleUser,
			AuthorKind: sessionmodel.AuthorUser,
			Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: params.Prompt}},
			CreatedAt:  at,
		})
		if err != nil {
			return err
		}
		committedMessage = &stored
		result.Task = task
		return nil
	})
	if err == nil && committedMessage != nil && !result.ExistingTask {
		if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hooks.CommittedEvent{
			Identity: hooks.Identity{
				SessionID: result.Task.SessionID,
				AgentID:   result.Task.AgentID,
				TaskID:    result.Task.ID,
			},
			Message: committedMessage,
		}); hookErr != nil {
			s.logger.Errorf("queued user message committed hook failed: %v", hookErr)
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

// Next 查询指定 Agent 的队首，不消费 Task；准备失败仍保持 Pending。
func (s *Service) Next(ctx context.Context, agentID contracts.AgentID) (taskmodel.Task, bool, error) {
	if ctx == nil {
		return taskmodel.Task{}, false, errors.New("next queued task context is required")
	}
	task, err := s.tasks.FindNextPendingByAgent(ctx, agentID)
	if errors.Is(err, contracts.ErrNotFound) {
		return taskmodel.Task{}, false, nil
	}
	return task, err == nil, err
}
