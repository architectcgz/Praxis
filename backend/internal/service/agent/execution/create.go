package execution

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/hook"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

// SendInput 是直接输入的身份、正文和模型选择，不接受调用方预构建的 Context。
type SendInput struct {
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	RequestID      identity.RequestID
	Content        string
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// SendInputResult 返回持久化 Execution 及请求是否已存在；重复请求不建立新的执行绑定。
type SendInputResult struct {
	Execution       agentmodel.Execution
	ExistingRequest bool
}

// SendInput 在 Agent 没有活动 Execution 时基于当前历史立即创建新的 Execution。
func (s *Service) SendInput(ctx context.Context, params SendInput) (SendInputResult, error) {
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

// createImmediate 在一次事务内完成新 Execution 的准入与执行绑定。
func (s *Service) createImmediate(ctx context.Context, params SendInput) (SendInputResult, error) {
	if ctx == nil {
		return SendInputResult{}, errors.New("task creation context is required")
	}
	var committedMessage *messagemodel.Message
	var result SendInputResult
	var admittedAgent identity.AgentID
	var admittedExecution identity.AgentExecutionID
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.records.FindByRequest(txCtx, params.AgentID, params.RequestID)
		if err == nil {
			if existing.SourceTaskID != "" ||
				existing.ProviderID != params.ProviderID || existing.ModelID != params.ModelID || existing.ReasoningLevel != params.ReasoningLevel ||
				params.SessionID != "" && existing.SessionID != params.SessionID {
				return fault.ErrRequestConflict
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
				return fault.ErrRequestConflict
			}
			result = SendInputResult{Execution: existing, ExistingRequest: true}
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		agent, err := s.agents.Get(txCtx, params.AgentID)
		if err != nil {
			return err
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, agent.SessionID); err != nil {
			return err
		}
		if params.SessionID != "" && agent.SessionID != params.SessionID {
			return fault.New(fault.InvalidRequest, "")
		}
		if !agent.State.Startable() {
			return fault.New(agentmodel.ExecutingCode, "")
		}
		active, err := s.records.CountActiveBySession(txCtx, agent.SessionID)
		if err != nil {
			return err
		}
		if active != 0 {
			return fault.New(agentmodel.ExecutingCode, "")
		}
		inputMessageID := "input:" + params.RequestID.String()
		config, err := s.buildConfig(txCtx, agent, params.ProviderID, params.ModelID, params.ReasoningLevel)
		if err != nil {
			return err
		}
		at := s.clock.Now()
		executionID := identity.AgentExecutionID(s.ids.New("execution"))
		value := messagemodel.Message{
			ID:          inputMessageID,
			RequestID:   params.RequestID.String(),
			ExecutionID: executionID.String(),
			Role:        messagemodel.RoleUser,
			AuthorKind:  messagemodel.AuthorUser,
			Blocks:      []messagemodel.Block{{Kind: messagemodel.BlockText, Text: params.Content}},
			CreatedAt:   at,
		}
		stored, _, err := s.messages.Append(txCtx, messagemodel.Record{Stream: agentservice.MessageStream(agent), AgentID: agent.ID, Data: value})
		if err != nil {
			return err
		}
		committedMessage = &stored
		selection, err := s.selectContext(txCtx, agent, config, inputMessageID)
		if err != nil {
			return err
		}
		task := agentmodel.NewExecution(executionID, agent.SessionID, agent.ID, params.RequestID, inputMessageID, config, selection, at)
		task.ProviderID = params.ProviderID
		task.ModelID = params.ModelID
		task.ReasoningLevel = params.ReasoningLevel
		if err := agent.StartExecution(task.ID, at); err != nil {
			return err
		}
		// 执行必须脱离事务 Context，避免 loop 后续写入已提交的事务工作区。
		_, created, err := s.executions.Reserve(ctx, agent.ID, task.ID)
		if err != nil {
			return err
		}
		if created {
			admittedAgent = agent.ID
			admittedExecution = task.ID
		}
		if err := s.records.Save(txCtx, task); err != nil {
			return err
		}
		if err := s.agents.Save(txCtx, agent); err != nil {
			return err
		}
		result.Execution = task
		return nil
	})
	if err != nil && admittedAgent != "" {
		s.executions.Release(admittedAgent, admittedExecution)
	}
	if err == nil && committedMessage != nil && !result.ExistingRequest {
		if hookErr := s.hookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
			Identity: hook.Identity{
				SessionID:   result.Execution.SessionID,
				AgentID:     result.Execution.AgentID,
				ExecutionID: result.Execution.ID,
			},
			Message: committedMessage,
		}); hookErr != nil {
			s.logger.Errorf("user message committed hook failed: %v", hookErr)
		}
	}
	return result, err
}

func (s *Service) inputMessageContent(ctx context.Context, agent agentmodel.Agent, requestID identity.RequestID) (string, error) {
	stored, err := s.messages.Get(ctx, agentservice.MessageStream(agent), "input:"+requestID.String())
	if err != nil {
		return "", err
	}
	if stored.Role != messagemodel.RoleUser {
		return "", fault.ErrRequestConflict
	}
	return stored.TextContent(), nil
}
