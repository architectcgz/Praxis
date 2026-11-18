package bindings

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/timing"
	"praxis/internal/dto"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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
}

func New(host Host) *Bindings {
	return &Bindings{
		host:     host,
		projects: &ProjectBindings{host: host},
		sessions: &SessionBindings{host: host},
		agents:   &AgentBindings{host: host},
		commands: &CommandBindings{host: host},
	}
}

func (b *Bindings) All() []application.Service {
	return []application.Service{
		application.NewService(b.projects),
		application.NewService(b.sessions),
		application.NewService(b.agents),
		application.NewService(b.commands),
	}
}

// EmitAgentEvent 显式映射核心载荷和条目，不向前端序列化领域对象。
func (b *Bindings) EmitAgentEvent(event coreevent.Event) {
	var data any
	switch payload := event.Payload.(type) {
	case coreevent.SessionConfigReloaded:
		data = struct{}{}
	case coreevent.AgentStateChanged:
		data = dto.AgentStateChanged{State: string(payload.State)}
	case coreevent.EntriesAppended:
		data = dto.AgentEntriesAppended{AgentID: event.Source.AgentID.String(),
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.TurnStarted, coreevent.TurnFailed, coreevent.TurnInterrupted, coreevent.SubagentReleased:
		data = struct{}{}
	case coreevent.ModelRequestStarted:
		data = dto.AgentModelRequestStarted{
			RetryCount:  payload.Request.RetryCount,
			RequestID:   payload.Request.ID.String(),
			RequestKind: string(payload.Request.Kind),
			Timing:      publicTiming(payload.Timing),
		}
	case coreevent.ModelRequestCompleted:
		data = publicRequestResult(payload.Request, payload.Timing)
	case coreevent.ModelRequestFailed:
		data = publicRequestResult(payload.Request, payload.Timing)
	case coreevent.ModelRequestInterrupted:
		data = publicRequestResult(payload.Request, payload.Timing)
	case coreevent.RunStarted:
		data = dto.AgentRunStarted{AgentID: event.Source.AgentID.String(),
			Run: publicRun(payload.Run),
		}
	case coreevent.RunInterruptRequested:
		data = dto.AgentRunControl{AppliedAt: payload.AppliedAt}
	case coreevent.SubagentStarted:
		record := payload.Run
		data = dto.AgentSubagentStarted{
			Agent: dto.AgentView{
				ID:           event.Source.AgentID.String(),
				SessionID:    event.Source.SessionID.String(),
				DefinitionID: payload.DefinitionID.String(),
				Kind:         "subagent",
				CreatedAt:    payload.CreatedAt,
				UpdatedAt:    payload.CreatedAt,
				Name:         payload.Name,
				State:        "executing",
				CurrentRunID: record.ID.String(),
				CurrentRun:   new(publicRun(record)),
			},
			Entries:       publicEntries(payload.Entries),
			ParentAgentID: payload.ParentAgentID.String(),
			ParentRunID:   payload.ParentRunID.String(),
			CallID:        payload.CallID,
		}
	case coreevent.TaskQueueChanged:
		tasks := make([]dto.QueuedTask, 0, len(payload.Tasks))
		for _, task := range payload.Tasks {
			tasks = append(tasks, dto.QueuedTask{
				ID:        task.ID.String(),
				Prompt:    task.Prompt,
				Sequence:  task.Sequence,
				CreatedAt: task.CreatedAt,
			})
		}
		data = dto.AgentTaskQueueChanged{Tasks: tasks, Paused: payload.Paused}
	case coreevent.TextDelta:
		data = dto.AgentTextDelta{Text: payload.Text}
	case coreevent.ThinkingDelta:
		data = dto.AgentThinkingDelta{Text: payload.Text}
	case coreevent.ToolCall:
		data = dto.AgentToolCall{
			CallID:    payload.CallID,
			Name:      payload.Name,
			Arguments: payload.Arguments,
		}
	case coreevent.ToolApprovalRequired:
		data = dto.AgentToolApprovalRequired{CallID: payload.CallID}
	case coreevent.ToolStarted:
		data = dto.AgentToolStarted{
			CallID: payload.CallID,
			Timing: publicTiming(payload.Timing),
		}
	case coreevent.ToolResult:
		data = dto.AgentToolResult{
			CallID:  payload.CallID,
			Result:  payload.Result,
			IsError: payload.IsError,
			Timing:  publicTiming(payload.Timing),
		}
	case coreevent.TurnCompleted:
		data = dto.AgentTurnCompleted{StopReason: string(payload.StopReason)}
	case coreevent.RunEnded:
		data = dto.AgentRunEnded{
			Run:     publicRun(payload.Run),
			State:   string(payload.State),
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.RequestCanceled:
		data = dto.AgentRunEnded{
			Run:     publicRun(payload.Run),
			State:   string(payload.State),
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.ErrorOccurred:
		data = dto.AgentErrorOccurred{Message: payload.Message}
	default:
		b.host.LogError("unsupported agent event payload: %T", event.Payload)
		return
	}
	b.host.EmitEvent("praxis:session:"+event.Source.SessionID.String(), dto.AgentEvent{
		Kind: string(event.Kind()),

		RunID:  event.Source.RunID.String(),
		TurnID: event.Source.TurnID.String(),
		Data:   data,
	})
}

func publicRequestResult(request sessionentry.ModelRequestRecord, measured *timing.Record) dto.AgentModelRequestResult {
	value := dto.AgentModelRequestResult{
		RequestID:   request.ID.String(),
		RequestKind: string(request.Kind),
		Status:      string(request.Status),
		RetryCount:  request.RetryCount,
		StopReason:  request.StopReason,
		Error:       request.Error,
		Timing:      publicTiming(measured),
	}
	if request.Usage != nil {
		value.Usage = new(publicUsage(*request.Usage))
	}
	return value
}
