package bindings

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	sessionservice "praxis/internal/service/session"
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
	case coreevent.SessionConfigReloaded:
		b.host.EmitEvent("praxis:session-config-reloaded", event.Source.SessionID.String())
		return
	case coreevent.EntriesAppended:
		data = dto.AgentEntriesAppended{
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.TurnStarted, coreevent.SubagentReleased:
		data = struct{}{}
	case coreevent.ModelRequestStarted:
		data = dto.AgentModelRequestStarted{
			RequestID:   payload.Request.ID.String(),
			RequestKind: string(payload.Request.Kind),
			Timing:      payload.Timing,
		}
	case coreevent.ModelRequestFinished:
		value := dto.AgentModelRequestFinished{
			RequestID:   payload.Request.ID.String(),
			RequestKind: string(payload.Request.Kind),
			StopReason:  string(payload.Request.StopReason),
			Timing:      payload.Timing,
		}
		if payload.Request.Usage != nil {
			value.Usage = new(publicUsage(*payload.Request.Usage))
		}
		data = value
	case coreevent.RunStarted:
		data = dto.AgentRunStarted{
			Run: payload.Run,
		}
	case coreevent.RunInterruptRequested:
		data = dto.AgentRunControl{AppliedAt: payload.AppliedAt}
	case coreevent.SubagentStarted:
		record := payload.Run
		data = dto.AgentSubagentStarted{
			Agent: sessionservice.AgentView{
				ID:           event.Source.AgentID,
				SessionID:    event.Source.SessionID,
				BranchID:     event.Source.BranchID,
				DefinitionID: payload.DefinitionID,
				Kind:         agentmodel.KindSubagent,
				CreatedAt:    payload.CreatedAt,
				UpdatedAt:    payload.CreatedAt,
				Name:         payload.Name,
				State:        agentmodel.AgentExecuting,
				CurrentRunID: record.ID,
				CurrentRun:   &record,
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
		data = dto.AgentTaskQueueChanged{Tasks: tasks}
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
			Timing: payload.Timing,
		}
	case coreevent.ToolResult:
		data = dto.AgentToolResult{
			CallID:  payload.CallID,
			Result:  payload.Result,
			IsError: payload.IsError,
			Timing:  payload.Timing,
		}
	case coreevent.TurnCompleted:
		data = dto.AgentTurnCompleted{StopReason: string(payload.StopReason)}
	case coreevent.RunEnded:
		data = dto.AgentRunEnded{
			Run:     payload.Run,
			State:   payload.State,
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.RequestCanceled:
		data = dto.AgentRunEnded{
			Run:     payload.Run,
			State:   payload.State,
			Entries: publicEntries(payload.Entries),
		}
	case coreevent.ErrorOccurred:
		data = dto.AgentErrorOccurred{Message: payload.Message}
	default:
		b.host.LogError("unsupported agent event payload: %T", event.Payload)
		return
	}
	b.host.EmitEvent(agentEventName, dto.AgentEvent{
		Kind:      string(event.Kind()),
		SessionID: event.Source.SessionID.String(),
		AgentID:   event.Source.AgentID.String(),
		BranchID:  event.Source.BranchID.String(),
		BindingID: event.Source.BindingID,
		RunID:     event.Source.RunID.String(),
		TurnID:    event.Source.TurnID.String(),
		Data:      data,
	})
}
