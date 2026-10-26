package bindings

import (
	"bytes"
	"context"
	coreevent "praxis/internal/core/event"
	"praxis/wails/dto"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const agentEventName = "praxis:agent-event"

type Host interface {
	BindingContext() (context.Context, Services, error)
	EmitEvent(string, any)
	SelectDirectory(string) (string, error)
	LogError(string, ...any)
}

type Bindings struct {
	host     Host
	projects *ProjectBindings
	sessions *SessionBindings
	agents   *AgentBindings
	commands *CommandBindings
	models   *ModelBindings
}

func New(host Host) *Bindings {
	return &Bindings{
		host:     host,
		projects: &ProjectBindings{host: host},
		sessions: &SessionBindings{host: host},
		agents:   &AgentBindings{host: host},
		commands: &CommandBindings{host: host},
		models:   &ModelBindings{host: host},
	}
}

func (b *Bindings) All() []application.Service {
	return []application.Service{
		application.NewService(b.projects),
		application.NewService(b.sessions),
		application.NewService(b.agents),
		application.NewService(b.commands),
		application.NewService(b.models),
	}
}

// EmitAgentEvent 显式映射核心载荷和条目，不向前端序列化领域对象。
func (b *Bindings) EmitAgentEvent(event coreevent.Event) {
	var data any
	switch payload := event.Payload.(type) {
	case coreevent.EntriesAppended:
		data = dto.AgentEntriesAppended{
			Entries:   publicEntries(payload.Entries),
			Transient: payload.Transient,
		}
	case coreevent.TurnStarted, coreevent.ProviderWaiting, coreevent.RequestCanceled:
		data = struct{}{}
	case coreevent.TextDelta:
		data = dto.AgentTextDelta{Text: payload.Text}
	case coreevent.ThinkingDelta:
		data = dto.AgentThinkingDelta{Text: payload.Text}
	case coreevent.ToolCall:
		data = dto.AgentToolCall{
			CallID: payload.CallID,
			Name:   payload.Name,
			Input:  bytes.Clone(payload.Input),
		}
	case coreevent.ToolApprovalRequired:
		data = dto.AgentToolApprovalRequired{
			CallID: payload.CallID,
			Name:   payload.Name,
			Input:  bytes.Clone(payload.Input),
		}
	case coreevent.ToolResult:
		data = dto.AgentToolResult{
			CallID:  payload.CallID,
			Result:  payload.Result,
			IsError: payload.IsError,
		}
	case coreevent.TurnCompleted:
		data = dto.AgentTurnCompleted{StopReason: string(payload.StopReason)}
	case coreevent.ModelUsage:
		data = dto.AgentModelUsage{Usage: publicUsage(payload.Usage)}
	case coreevent.ExecutionEnded:
		data = dto.AgentExecutionEnded{
			Outcome:        payload.Outcome,
			FailureCode:    payload.FailureCode,
			FailureMessage: payload.FailureMessage,
		}
	case coreevent.OperationTiming:
		data = dto.AgentOperationTiming{Timing: operationTiming(payload.Timing)}
	case coreevent.ErrorOccurred:
		data = dto.AgentErrorOccurred{Message: payload.Message}
	default:
		b.host.LogError("unsupported agent event payload: %T", event.Payload)
		return
	}
	b.host.EmitEvent(agentEventName, dto.AgentEvent{
		Kind:        string(event.Kind()),
		SessionID:   event.Scope.SessionID.String(),
		AgentID:     event.Scope.AgentID.String(),
		ExecutionID: event.Scope.ExecutionID.String(),
		TurnID:      event.Scope.TurnID.String(),
		Data:        data,
	})
}
