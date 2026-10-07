// Package queue 负责 runtime 的持久化任务队列，保存待执行内容并按 FIFO 提供下一项。
package queue

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/repository"
	"praxis/internal/system"

	"context"
	"errors"
	"fmt"
)

type Config struct {
	Transactions repository.TxRunner
	Agents       repository.SessionAgentRepository
	Tasks        repository.TaskRepository
	Messages     repository.MessageStreams
	Clock        system.Clock
}

type Service struct {
	tx       repository.TxRunner
	agents   repository.SessionAgentRepository
	tasks    repository.TaskRepository
	messages repository.MessageStreams
	clock    system.Clock
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
		tx:       config.Transactions,
		agents:   config.Agents,
		tasks:    config.Tasks,
		messages: config.Messages,
		clock:    system.ClockOrDefault(config.Clock),
	}, nil
}

// EnqueueTask 在 main agent 忙碌时预约 Task；同一请求返回原 Task，输入不同则拒绝。
func (s *Service) EnqueueTask(ctx context.Context, params EnqueueParams) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue task context is required")
	}
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
			params.ID, agent.SessionID, agent.ID, params.RequestID, sequence, at,
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
		_, err = s.messages.Append(txCtx, agent, sessionmodel.MessageData{
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
		result.Task = task
		return nil
	})
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

// TaskQueue 保存所属 runtime 的待执行内容；存储范围在装配时确定，读取时无需指定执行者。
type TaskQueue struct {
	tasks       repository.TaskRepository
	mainAgentID contracts.AgentID
}

// NewTaskQueue 创建 runtime 持有的任务队列；mainAgentID 仅用于隔离持久化任务，不负责分配任务。
func NewTaskQueue(tasks repository.TaskRepository, mainAgentID contracts.AgentID) (*TaskQueue, error) {
	if tasks == nil || mainAgentID == "" {
		return nil, errors.New("task queue repository and main agent id are required")
	}
	return &TaskQueue{tasks: tasks, mainAgentID: mainAgentID}, nil
}

// Next 返回最早的待执行任务；队列为空时返回 false。
// 查询不消费 Task，runtime 构建 Context 成功后原子推进状态，失败保持 Pending。
func (q *TaskQueue) Next(ctx context.Context) (taskmodel.Task, bool, error) {
	if ctx == nil {
		return taskmodel.Task{}, false, errors.New("next queued task context is required")
	}
	task, err := q.tasks.FindNextPendingByAgent(ctx, q.mainAgentID)
	if errors.Is(err, contracts.ErrNotFound) {
		return taskmodel.Task{}, false, nil
	}
	return task, err == nil, err
}
