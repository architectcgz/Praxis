package compose

import (
	"bytes"

	agentcontracts "praxis/internal/app/agent/contracts"
	appusage "praxis/internal/app/usage"
	"praxis/internal/loop"
	"praxis/internal/service/task"
)

// desktopAgentEvent 将执行循环事件转换为公开通知，Loop 不依赖应用契约。
func desktopAgentEvent(event loop.Event) agentcontracts.AgentEvent {
	result := agentcontracts.AgentEvent{
		Kind:      string(event.Kind),
		SessionID: event.SessionID.String(),
		AgentID:   event.AgentID.String(),
		TaskID:    event.TaskID.String(),
		TurnID:    event.TurnID.String(),
		Text:      event.Text,
		CallID:    event.CallID,
		Name:      event.Name,
		Input:     bytes.Clone(event.Input),
		Result:    event.Result,
		IsError:   event.IsError,
		Error:     event.Error,
	}
	if event.Usage != nil {
		usage := appusage.PublicUsage(*event.Usage)
		result.Usage = &usage
	}
	return result
}

func terminalAgentEvent(event task.TerminalEvent) agentcontracts.AgentEvent {
	return agentcontracts.AgentEvent{
		Kind:           event.Kind,
		SessionID:      event.SessionID.String(),
		AgentID:        event.AgentID.String(),
		TaskID:         event.TaskID.String(),
		Outcome:        event.Outcome,
		FailureCode:    string(event.FailureCode),
		FailureMessage: event.FailureMessage,
	}
}
