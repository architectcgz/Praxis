package compose

import (
	"bytes"

	"praxis/internal/loop"
	appservices "praxis/internal/service"
	"praxis/internal/service/task"
	"praxis/internal/timing"
)

// desktopAgentEvent 将 Loop 事件转换为稳定的前端通知。
func desktopAgentEvent(event loop.Event) appservices.AgentEvent {
	result := appservices.AgentEvent{
		Kind:      string(event.Kind),
		SessionID: event.SessionID,
		AgentID:   event.AgentID,
		TaskID:    event.TaskID,
		TurnID:    event.TurnID,
		Text:      event.Text,
		CallID:    event.CallID,
		Name:      event.Name,
		Input:     bytes.Clone(event.Input),
		Result:    event.Result,
		IsError:   event.IsError,
		Error:     event.Error,
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

func terminalAgentEvent(event task.TerminalEvent) appservices.AgentEvent {
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
