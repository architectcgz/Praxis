package compose

import (
	"bytes"

	"praxis/internal/agent_runtime"
	appservices "praxis/internal/service"
	"praxis/internal/service/runtime/task/lifecycle"
	"praxis/internal/timing"
)

// desktopAgentEvent 是 runtime 事件进入 application 边界的唯一转换入口。
func desktopAgentEvent(event agentruntime.AgentEvent) appservices.AgentEvent {
	result := appservices.AgentEvent{
		Kind:           string(event.Kind),
		SessionID:      event.SessionID,
		AgentID:        event.AgentID,
		TaskID:         event.TaskID,
		Outcome:        string(event.Outcome),
		FailureCode:    string(event.FailureCode),
		FailureMessage: event.FailureMessage,
		TurnID:         event.TurnID,
		Text:           event.Text,
		CallID:         event.CallID,
		Name:           event.Name,
		Input:          bytes.Clone(event.Input),
		Result:         event.Result,
		IsError:        event.IsError,
		Error:          event.Error,
	}
	if event.Timing != nil {
		value := desktopTiming(*event.Timing)
		result.Timing = &value
	}
	if event.Usage != nil {
		result.Usage = &appservices.ModelUsage{
			InputTokens:              event.Usage.InputTokens,
			OutputTokens:             cloneValue(event.Usage.OutputTokens),
			CacheReadInputTokens:     cloneValue(event.Usage.CacheReadInputTokens),
			CacheCreationInputTokens: cloneValue(event.Usage.CacheCreationInputTokens),
		}
	}
	return result
}

func terminalAgentEvent(event lifecycle.TerminalEvent) appservices.AgentEvent {
	return appservices.AgentEvent{
		Kind:           event.Kind,
		SessionID:      event.SessionID,
		AgentID:        event.AgentID,
		TaskID:         event.TaskID,
		Outcome:        event.Outcome,
		FailureCode:    string(event.FailureCode),
		FailureMessage: event.FailureMessage,
	}
}

func desktopTiming(record timing.Record) appservices.OperationTiming {
	return appservices.OperationTiming{
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
		DurationMS:      cloneValue(record.DurationMS),
		FirstResponseMS: cloneValue(record.FirstResponseMS),
		Status:          string(record.Status),
		Revision:        record.Revision,
	}
}

func cloneValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
