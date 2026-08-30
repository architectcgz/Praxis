package app

import (
	"context"
	"sort"

	"praxis/internal/contracts"
	"praxis/internal/core/domain"
)

type AgentBindings struct {
	runtime *bindingRuntime
	queries serviceRef[AgentQueries]
}

func (b *AgentBindings) GetAgent(agentID string) (response contracts.AgentSnapshot, err error) {
	done := b.runtime.begin("GetAgent")
	defer func() { done(err) }()
	ctx, queries, err := b.bindingContext()
	if err != nil {
		return contracts.AgentSnapshot{}, err
	}
	projection, err := queries.ProjectAgent(ctx, domain.AgentID(agentID), 100)
	if err != nil {
		return contracts.AgentSnapshot{}, publicBindingError(err)
	}
	result := contracts.AgentSnapshot{
		ID:                projection.Agent.ID.String(),
		SessionID:         projection.Agent.SessionID.String(),
		GroupID:           projection.Agent.GroupID.String(),
		Profile:           string(projection.Agent.Profile),
		State:             string(projection.Agent.State),
		CurrentExecution:  projection.Agent.CurrentExecutionID.String(),
		ExecutionIDs:      make([]string, 0, len(projection.Executions)),
		Executions:        make([]contracts.ExecutionSnapshot, 0, len(projection.Executions)),
		WaitConditionIDs:  make([]string, 0, len(projection.Waits)),
		DeliveryIDs:       make([]string, 0, len(projection.Deliveries)),
		ControlRequestIDs: make([]string, 0, len(projection.Controls)),
	}
	for _, execution := range projection.Executions {
		result.ExecutionIDs = append(result.ExecutionIDs, execution.ID.String())
		result.Executions = append(result.Executions, contracts.ExecutionSnapshot{
			ID:          execution.ID.String(),
			Reason:      string(execution.Reason),
			Status:      string(execution.Status),
			Outcome:     string(execution.Outcome),
			FailureCode: string(execution.FailureCode),
			CreatedAt:   execution.CreatedAt,
			StartedAt:   execution.StartedAt,
			SettledAt:   execution.SettledAt,
		})
	}
	for _, wait := range projection.Waits {
		result.WaitConditionIDs = append(result.WaitConditionIDs, wait.ID.String())
	}
	for _, delivery := range projection.Deliveries {
		result.DeliveryIDs = append(result.DeliveryIDs, delivery.ID.String())
	}
	for _, control := range projection.Controls {
		result.ControlRequestIDs = append(result.ControlRequestIDs, control.ID.String())
	}
	return result, nil
}

func (b *AgentBindings) ListAgentMessages(
	agentID string,
) (response []contracts.AgentMessage, err error) {
	done := b.runtime.begin("ListAgentMessages")
	defer func() { done(err) }()
	ctx, queries, err := b.bindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := queries.ListAgentMessages(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.AgentMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, contracts.AgentMessage{
			Sequence:    message.Sequence,
			At:          message.At,
			ExecutionID: message.ExecutionID.String(),
			Role:        message.Role,
			Content:     message.Content,
		})
	}
	return result, nil
}

func (b *AgentBindings) ListAgentHistory(
	agentID string,
) (response []contracts.AgentHistoryItem, err error) {
	done := b.runtime.begin("ListAgentHistory")
	defer func() { done(err) }()
	ctx, queries, err := b.bindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := queries.ListAgentMessages(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	projection, err := queries.ProjectAgent(ctx, domain.AgentID(agentID), 200)
	if err != nil {
		return nil, publicBindingError(err)
	}
	result := make([]contracts.AgentHistoryItem, 0, len(messages)+len(projection.Executions))
	for _, message := range messages {
		value := contracts.AgentMessage{
			Sequence:    message.Sequence,
			At:          message.At,
			ExecutionID: message.ExecutionID.String(),
			Role:        message.Role,
			Content:     message.Content,
		}
		result = append(result, contracts.AgentHistoryItem{
			Kind:     "message",
			At:       message.At,
			Sequence: message.Sequence,
			Message:  &value,
		})
	}
	for _, execution := range projection.Executions {
		if execution.Status != domain.ExecutionSettled || execution.Outcome != domain.ExecutionFailed {
			continue
		}
		at := execution.SettledAt
		if at.IsZero() {
			at = execution.CreatedAt
		}
		value := contracts.ExecutionSnapshot{
			ID:          execution.ID.String(),
			Reason:      string(execution.Reason),
			Status:      string(execution.Status),
			Outcome:     string(execution.Outcome),
			FailureCode: string(execution.FailureCode),
			CreatedAt:   execution.CreatedAt,
			StartedAt:   execution.StartedAt,
			SettledAt:   execution.SettledAt,
		}
		result = append(result, contracts.AgentHistoryItem{
			Kind:      "execution",
			At:        at,
			Execution: &value,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].At.Equal(result[j].At) {
			return result[i].Sequence < result[j].Sequence
		}
		return result[i].At.Before(result[j].At)
	})
	return result, nil
}

func (b *AgentBindings) bindingContext() (context.Context, AgentQueries, error) {
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, nil, err
	}
	queries := b.queries.get()
	if queries == nil {
		return nil, nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	return ctx, queries, nil
}
