package service

import (
	"context"
	"errors"
	agentmodel "praxis/internal/agent"
	"praxis/internal/fault"
	"praxis/internal/hook"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	taskmodel "praxis/internal/task"
)

type EnqueueInput struct {
	ID             identity.TaskID
	RequestID      identity.RequestID
	AgentID        identity.AgentID
	Prompt         string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

type EnqueueResult struct {
	Task         taskmodel.Task
	ExistingTask bool
}

// EnqueueTask 在 main agent 忙碌时预约 Task；同一请求返回原 Task，输入不同则拒绝。
func (s *Service) EnqueueTask(ctx context.Context, params EnqueueInput) (EnqueueResult, error) {
	if ctx == nil {
		return EnqueueResult{}, errors.New("enqueue task context is required")
	}
	var committedMessage *messagemodel.Message
	result := EnqueueResult{}
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if !agent.CanReadSessionContext() {
			return fault.New(fault.InvalidRequest, "queued input requires the session main agent")
		}
		existingRequest, err := s.tasks.FindByRequest(txCtx, agent.ID, params.RequestID)
		if err == nil {
			if existingRequest.ID != params.ID || existingRequest.Sequence == 0 ||
				existingRequest.ProviderID != params.ProviderID || existingRequest.ModelID != params.ModelID || existingRequest.ReasoningLevel != params.ReasoningLevel {
				return fault.ErrRequestConflict
			}
			storedContent, err := s.inputMessageContent(txCtx, agent, existingRequest.RequestID)
			if err != nil {
				return err
			}
			if storedContent != params.Prompt {
				return fault.ErrRequestConflict
			}
			result.Task, result.ExistingTask = existingRequest, true
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		_, err = s.tasks.Get(txCtx, params.ID)
		if err == nil {
			return fault.ErrRequestConflict
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		if agent.State != agentmodel.AgentExecuting || agent.CurrentExecutionID == "" {
			return fault.New(agentmodel.UnavailableCode, "queue input requires an executing agent")
		}
		sequence, err := s.tasks.NextSequence(txCtx, agent.ID)
		if err != nil {
			return err
		}
		at := s.clock.Now().UTC()
		task := taskmodel.NewTask(
			params.ID, agent.SessionID, agent.ID, params.RequestID, "input:"+params.RequestID.String(), sequence, at,
		)
		task.ProviderID = params.ProviderID
		task.ModelID = params.ModelID
		task.ReasoningLevel = params.ReasoningLevel
		if err := s.tasks.Save(txCtx, task); err != nil {
			return err
		}
		stored, err := s.messages.Append(txCtx, agent.SessionID, agent.ID, messagemodel.Message{
			ID:           "input:" + params.RequestID.String(),
			RequestID:    params.RequestID.String(),
			SourceTaskID: task.ID.String(),
			Role:         messagemodel.RoleUser,
			AuthorKind:   messagemodel.AuthorUser,
			Blocks:       []messagemodel.Block{{Kind: messagemodel.BlockText, Text: params.Prompt}},
			CreatedAt:    at,
		})
		if err != nil {
			return err
		}
		committedMessage = &stored
		result.Task = task
		return nil
	})
	if err == nil && committedMessage != nil && !result.ExistingTask {
		if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
			Identity: hook.Identity{
				SessionID:    result.Task.SessionID,
				AgentID:      result.Task.AgentID,
				SourceTaskID: result.Task.ID,
			},
			Message: committedMessage,
		}); hookErr != nil {
			s.logger.Errorf("queued user message committed hook failed: %v", hookErr)
		}
	}
	return result, err
}

// Next 查询指定 Agent 的队首，不消费 Task；准备失败仍保持 Pending。
func (s *Service) Next(ctx context.Context, agentID identity.AgentID) (taskmodel.Task, bool, error) {
	if ctx == nil {
		return taskmodel.Task{}, false, errors.New("next queued task context is required")
	}
	task, err := s.tasks.FindNextPendingByAgent(ctx, agentID)
	if errors.Is(err, fault.ErrNotFound) {
		return taskmodel.Task{}, false, nil
	}
	return task, err == nil, err
}
